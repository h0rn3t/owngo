package census

import (
	"testing"

	"golang.org/x/tools/go/ssa"
)

const builderSrc = `package p

import "unsafe"

type Tree struct {
	Left, Right *Tree
	Val         int
}

type Tagged struct {
	Next *Tagged
	Tag  []byte
}

type sizer interface{ size() int }

var count int

// NewTree — binary-trees Go #2.
func NewTree(depth int) *Tree {
	if depth > 0 {
		return &Tree{Left: NewTree(depth - 1), Right: NewTree(depth - 1)}
	} else {
		return &Tree{}
	}
}

func even(d int) *Tree {
	if d <= 0 {
		return &Tree{}
	}
	return &Tree{Left: odd(d - 1)}
}

func odd(d int) *Tree {
	return &Tree{Left: even(d - 1), Val: d}
}

func paramInNode(t *Tree) *Tree { return &Tree{Left: t} }

func droppedChild() *Tree {
	NewTree(1)
	return &Tree{}
}

func sameChild() *Tree {
	t := NewTree(1)
	return &Tree{Left: t, Right: t}
}

func withMake(d int) *Tree { return &Tree{Val: len(make([]int, d))} }

func withGo() *Tree {
	go func() {}()
	return &Tree{}
}

func withGlobal() *Tree {
	count++
	return &Tree{}
}

func viaIface(s sizer) *Tree { return &Tree{Val: s.size()} }

func callsNonBuilder() *Tree { return &Tree{Left: withGlobal()} }

func interior() *int {
	t := &Tree{}
	return &t.Val
}

func newTagged() *Tagged { return &Tagged{} }

func writeParam(t *Tree) *Tree {
	t.Val = 1
	return &Tree{}
}

func withChan(c chan int) *Tree {
	c <- 1
	return &Tree{}
}

func recv(c chan int) *Tree { return &Tree{Val: <-c} }

func concat(s string) *Tree { return &Tree{Val: len(s + "x")} }

func viaUnsafe(p *int) *Tree { return &Tree{Val: int(uintptr(unsafe.Pointer(p)))} }
`

// TestBuilder — P-NEW-05.1: будівник — функція зі свіжим результатом, чия
// структура зібрана лише з її власних Alloc і переміщених результатів
// будівників. Колонка fresh показує, що відмова — через умову будівника,
// а не через P6.5.
func TestBuilder(t *testing.T) {
	for _, mode := range []ssa.BuilderMode{ssa.SanityCheckFunctions, ssa.SanityCheckFunctions | ssa.GlobalDebug} {
		pkg := buildOnePackage(t, "example.com/p", builderSrc, mode)
		sums := packageSummaries(nil, ssaFuncs(pkg))
		tests := []struct {
			fn      string
			fresh   bool
			builder bool
		}{
			{"NewTree", true, true},
			{"even", true, true},
			{"odd", true, true},
			{"paramInNode", false, false},
			{"droppedChild", true, false},
			{"sameChild", false, false},
			{"withMake", true, false},
			{"withGo", true, false},
			{"withGlobal", true, false},
			{"viaIface", true, false},
			{"callsNonBuilder", true, false},
			{"interior", true, false},
			{"newTagged", true, false},
			{"writeParam", true, false},
			{"withChan", true, false},
			{"recv", true, false},
			{"concat", true, false},
			{"viaUnsafe", true, false},
		}
		for _, tt := range tests {
			sum := sums[pkg.Func(tt.fn)]
			if sum.Fresh != tt.fresh || sum.Builder != tt.builder {
				t.Errorf("mode %v: packageSummaries(%s) Fresh, Builder = %v, %v, want %v, %v",
					mode, tt.fn, sum.Fresh, sum.Builder, tt.fresh, tt.builder)
			}
		}
	}
}
