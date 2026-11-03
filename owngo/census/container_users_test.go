package census

import (
	"slices"
	"testing"

	"golang.org/x/tools/go/ssa"
)

// GOROOT/test bug054.go: зріз лежить у полі v, v — у полі s, а виклик
// read(s) читає зріз через s. Вживання місця — і читачі кожного
// контейнера, що його тримає (транзитивно): звільнення після виклику.
const containerUsersSrc = `package p

type V struct{ e []int }
type S struct{ v *V }

func read(s *S) int { return len(s.v.e) }

func nested(n int) int {
	v := new(V)
	v.e = make([]int, n)
	s := new(S)
	s.v = v
	return read(s)
}
`

func TestContainerReadersAreSiteUses(t *testing.T) {
	pkg := buildOnePackage(t, "example.com/p", containerUsersSrc, ssa.SanityCheckFunctions)
	place := PlaceAll(ssaFuncs(pkg))
	fn := pkg.Func("nested")
	var slice, call ssa.Instruction
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			switch instr := instr.(type) {
			case *ssa.MakeSlice:
				slice = instr
			case *ssa.Call:
				call = instr
			}
		}
	}
	if slice == nil || call == nil {
		t.Fatalf("nested: make %v, call %v; want both", slice, call)
	}
	uses := append(slices.Clone(place.StaticUses[slice]), place.DecUses[slice]...)
	if len(uses) > 0 && !slices.Contains(uses, call) {
		t.Errorf("PlaceAll(nested): make %q uses = %v, want them to include %v", place.Cats[slice], uses, call)
	}
}
