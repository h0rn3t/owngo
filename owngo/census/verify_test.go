package census

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/ssa"
)

const verifySrc = `package p

type Node struct{ Next *Node }

func keep(n *Node) {}

func static(c bool) int {
	p := new(int)
	*p = 1
	if c {
		*p = 2
	}
	return *p
}

func passed() {
	n := new(Node)
	keep(n)
}
`

// TestVerify — P-NEW-03.1: верифікатор називає кожне порушення зіпсованого
// розміщення і мовчить на правильному.
func TestVerify(t *testing.T) {
	pkg := buildOnePackage(t, "p", verifySrc, ssa.BuilderMode(0))
	static := pkg.Func("static")
	site := findInstr[*ssa.Alloc](t, static)
	store := findInstr[*ssa.Store](t, static)
	ret := findInstr[*ssa.Return](t, static)
	load := ret.Results[0].(*ssa.UnOp)
	uses := map[ssa.Instruction][]ssa.Instruction{site: usersOf(site)}
	ifBlock := store.Block().Succs[0] // гілка c: *p = 2

	passed := pkg.Func("passed")
	node := findInstr[*ssa.Alloc](t, passed)
	call := findInstr[*ssa.Call](t, passed)

	tests := []struct {
		name string
		fn   *ssa.Function
		own  Ownership
		want []string // підрядки порушень у порядку Verify
	}{
		{
			name: "valid",
			fn:   static,
			own:  Ownership{Uses: uses, After: map[ssa.Instruction][]ssa.Value{load: {site}}},
		},
		{
			name: "double release",
			fn:   static,
			own: Ownership{Uses: uses, After: map[ssa.Instruction][]ssa.Value{
				store: {site},
				load:  {site},
			}},
			// *p = 2 у гілці і читання після неї бачать звільнений об'єкт.
			want: []string{"use after release", "use after release", "double release"},
		},
		{
			name: "double release on one branch",
			fn:   static,
			own: Ownership{
				Uses:  uses,
				After: map[ssa.Instruction][]ssa.Value{load: {site}},
				Entry: map[*ssa.BasicBlock][]ssa.Value{ifBlock: {site}},
			},
			want: []string{"use after release", "use after release", "double release"},
		},
		{
			name: "inc without pair",
			fn:   passed,
			own:  Ownership{Inc: map[ssa.Instruction][]ssa.Value{call: {node}}},
			want: []string{"inc without pair"},
		},
		{
			name: "inc paired with owner dec",
			fn:   passed,
			own: Ownership{
				Uses:  map[ssa.Instruction][]ssa.Instruction{node: {call}},
				After: map[ssa.Instruction][]ssa.Value{call: {node}},
				Inc:   map[ssa.Instruction][]ssa.Value{call: {node}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Verify(tt.fn, tt.own)
			if len(got) != len(tt.want) {
				t.Fatalf("Verify(%s) = %v, want %d violations %q", tt.fn.Name(), got, len(tt.want), tt.want)
			}
			for i, v := range got {
				if !strings.Contains(v.Error(), tt.want[i]) {
					t.Errorf("Verify(%s)[%d] = %q, want it to contain %q", tt.fn.Name(), i, v.Error(), tt.want[i])
				}
				if len(v.Path) == 0 {
					t.Errorf("Verify(%s)[%d] = %q has no path", tt.fn.Name(), i, v.Error())
				}
			}
		})
	}
}

func findInstr[T ssa.Instruction](t *testing.T, fn *ssa.Function) T {
	t.Helper()
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			if v, ok := instr.(T); ok {
				return v
			}
		}
	}
	var zero T
	t.Fatalf("%s has no %T", fn.Name(), zero)
	return zero
}

func usersOf(v ssa.Value) []ssa.Instruction {
	return append([]ssa.Instruction(nil), *v.Referrers()...)
}
