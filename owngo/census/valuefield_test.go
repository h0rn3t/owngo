package census

import (
	"slices"
	"testing"

	"golang.org/x/tools/go/ssa"
)

// GOROOT/test ddd.go: s.Sum() на структурі з вбудованим інтерфейсом — це
// поле значення (ssa.Field) і invoke. Поле і елемент значення-масиву
// (ssa.Index) несуть ті самі вказівники, що й значення: u живе до invoke.
const valueFieldSrc = `package p

type I interface{ Sum(...int) int }

type U struct{ n *int }

func (u *U) Sum(args ...int) int { return len(args) }

func field() int {
	var u U
	var s struct{ I }
	s.I = &u
	return s.Sum(1, 2)
}

func index() int {
	var u U
	a := [1]I{&u}
	return a[0].Sum(1)
}
`

func TestValueFieldCarriesPointers(t *testing.T) {
	pkg := buildOnePackage(t, "example.com/p", valueFieldSrc, ssa.SanityCheckFunctions)
	place := PlaceAll(ssaFuncs(pkg))
	for _, name := range []string{"field", "index"} {
		var u, invoke ssa.Instruction
		for _, b := range pkg.Func(name).Blocks {
			for _, instr := range b.Instrs {
				switch instr := instr.(type) {
				case *ssa.Alloc:
					if instr.Comment == "u" {
						u = instr
					}
				case *ssa.Call:
					if instr.Call.IsInvoke() {
						invoke = instr
					}
				}
			}
		}
		if u == nil || invoke == nil {
			t.Fatalf("%s: u %v, invoke %v; want both", name, u, invoke)
		}
		uses := append(slices.Clone(place.StaticUses[u]), place.DecUses[u]...)
		if len(uses) > 0 && !slices.Contains(uses, invoke) {
			t.Errorf("PlaceAll(%s): u %q uses = %v, want them to include %v", name, place.Cats[u], uses, invoke)
		}
	}
}
