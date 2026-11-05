package cl

import (
	"testing"

	"github.com/xgo-dev/llgo/owngo/census"
	gossa "golang.org/x/tools/go/ssa"
)

const ownGoRegionSrc = `package foo

import "unsafe"

type Tree struct {
	Left, Right *Tree
}

var (
	g     *Tree
	count int
)

func NewTree(depth int) *Tree {
	if depth > 0 {
		return &Tree{Left: NewTree(depth - 1), Right: NewTree(depth - 1)}
	}
	return &Tree{}
}

func (t *Tree) Count() int {
	if t.Left == nil {
		return 1
	}
	return 1 + t.Right.Count() + t.Left.Count()
}

func graft(t, n *Tree) { t.Left = n }

func use(t *Tree) int {
	if t == nil {
		return 0
	}
	return 1
}

func counting(depth int) *Tree {
	count++
	return NewTree(depth)
}

func counted(depth int) int {
	a := NewTree(depth)
	return a.Count()
}

func childAfter(depth int) bool {
	a := NewTree(depth)
	l := a.Left
	return l != nil && l.Left == nil
}

func loop(n, depth int) int {
	acc := 0
	for i := 0; i < n; i++ {
		a := NewTree(depth)
		acc += a.Count()
	}
	return acc
}

func deferred(depth int) {
	a := NewTree(depth)
	defer use(a)
}

func grafted(depth int) int {
	a := NewTree(depth)
	graft(a, g)
	return a.Count()
}

func stashed(depth int) {
	g = NewTree(depth)
}

func returned(depth int) *Tree {
	return NewTree(depth)
}

func returnsLeft(depth int) *Tree {
	a := NewTree(depth)
	return a.Left
}

func childGo(depth int) {
	a := NewTree(depth)
	go use(a.Left)
}

func childChan(depth int, c chan *Tree) {
	a := NewTree(depth)
	c <- a.Left
}

func childGlobal(depth int) {
	a := NewTree(depth)
	g = a.Left
}

func childUnsafe(depth int) uintptr {
	a := NewTree(depth)
	return uintptr(unsafe.Pointer(a.Left))
}

func writeNil(depth int) int {
	a := NewTree(depth)
	a.Left = nil
	return a.Count()
}

func carried(n, depth int) int {
	var prev *Tree
	acc := 0
	for i := 0; i < n; i++ {
		a := NewTree(depth)
		if prev != nil {
			acc += prev.Count()
		}
		prev = a
	}
	return acc
}

func deferWrite(depth int) {
	a := NewTree(depth)
	defer graft(a, nil)
}

func writeChild(depth int) int {
	a := NewTree(depth)
	l := a.Left
	l.Right = nil
	return a.Count()
}

func id(t *Tree) *Tree { return t }

func aliased(depth int) bool {
	a := NewTree(depth)
	return id(a) == nil
}

func notBuilder(depth int) int {
	a := counting(depth)
	return a.Count()
}
`

// TestOwnGoRegionRoot — P-NEW-05.2: виклик будівника, якому P6.6 ставить
// DecCyclic, — корінь регіону, якщо дерево після побудови лише читають.
// Позначка коду не змінює.
func TestOwnGoRegionRoot(t *testing.T) {
	ssaPkg, _, _ := buildGoSSAPkg(t, ownGoRegionSrc)
	place := census.PlaceAll(ssaPackageFuncs(ssaPkg))
	tests := []struct {
		fn   string
		root bool
	}{
		{"counted", true},
		{"childAfter", true},
		{"loop", true},
		{"deferred", true},
		{"grafted", false},
		{"stashed", false},
		{"returned", false},
		{"returnsLeft", false},
		{"childGo", false},
		{"childChan", false},
		{"childGlobal", false},
		{"childUnsafe", false},
		{"writeNil", false},
		{"carried", false},
		{"deferWrite", false},
		{"writeChild", false},
		{"aliased", false},
		{"notBuilder", false},
	}
	for _, tt := range tests {
		var roots []gossa.Instruction
		for _, block := range ssaPkg.Func(tt.fn).Blocks {
			for _, instr := range block.Instrs {
				if _, ok := place.Regions[instr]; ok {
					roots = append(roots, instr)
				}
			}
		}
		if got := len(roots) == 1; got != tt.root || len(roots) > 1 {
			t.Errorf("PlaceAll(%s) region roots = %v, want root %v", tt.fn, roots, tt.root)
		}
	}
}
