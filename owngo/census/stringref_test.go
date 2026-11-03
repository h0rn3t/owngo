package census

import (
	"slices"
	"testing"

	"golang.org/x/tools/go/ssa"
)

// GOROOT/test bug501.go: під owngo рядок з конкатенації — купове місце.
// Параметр-рядок, який функція повертає, тече в результат, а рядок,
// прочитаний з поля, — той самий рядок, що туди записали.
const stringRefSrc = `package p

type T struct{ name string }

func F(s string) (string, error) { return s, nil }

func field(a, b string) int {
	t := new(T)
	t.name = a + b
	n := t.name
	return len(n)
}
`

func TestStringParamAliasesResult(t *testing.T) {
	pkg := buildOnePackage(t, "example.com/p", stringRefSrc, ssa.SanityCheckFunctions)
	sum := packageSummaries(nil, ssaFuncs(pkg))[pkg.Func("F")]
	if sum == nil || len(sum.Params) != 1 || !slices.Equal(sum.Params[0].Alias, []int{0}) {
		t.Errorf("packageSummaries(F) = %+v, want Params[0].Alias [0]", sum)
	}

	place := PlaceAll(ssaFuncs(pkg))
	var concat, length ssa.Instruction
	for _, b := range pkg.Func("field").Blocks {
		for _, instr := range b.Instrs {
			switch instr := instr.(type) {
			case *ssa.BinOp:
				concat = instr
			case *ssa.Call:
				length = instr
			}
		}
	}
	if concat == nil || length == nil {
		t.Fatalf("field: concat %v, len %v; want both", concat, length)
	}
	uses := append(slices.Clone(place.StaticUses[concat]), place.DecUses[concat]...)
	if len(uses) > 0 && !slices.Contains(uses, length) {
		t.Errorf("PlaceAll(field): concat %q uses = %v, want them to include %v", place.Cats[concat], uses, length)
	}
}
