package census

import (
	"go/token"
	"slices"
	"testing"

	"golang.org/x/tools/go/ssa"
)

// GOROOT/test unsafe_string.go, unsafebuiltins.go: unsafe.String і
// unsafe.Slice дають значення в пам'яті першого аргументу. Масив живе, доки
// живе рядок, і параметр, з якого їх зроблено, тече в результат.
const unsafeStringSrc = `package p

import "unsafe"

func local(n int) bool {
	b := make([]byte, n)
	s := unsafe.String(&b[0], n)
	return s == "x"
}

func viaSlice(n int) int {
	b := make([]byte, n)
	t := unsafe.Slice(&b[0], n)
	return len(t) + int(t[0])
}

func toString(b []byte) string { return unsafe.String(unsafe.SliceData(b), len(b)) }
`

func TestUnsafeStringAliasesArgument(t *testing.T) {
	pkg := buildOnePackage(t, "example.com/p", unsafeStringSrc, ssa.SanityCheckFunctions)
	place := PlaceAll(ssaFuncs(pkg))
	for _, name := range []string{"local", "viaSlice"} {
		fn := pkg.Func(name)
		var buf, last ssa.Instruction
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				switch instr := instr.(type) {
				case *ssa.MakeSlice:
					buf = instr
				case *ssa.BinOp:
					if instr.Op == token.EQL {
						last = instr
					}
				case *ssa.UnOp:
					if instr.Op == token.MUL {
						last = instr
					}
				}
			}
		}
		if buf == nil || last == nil {
			t.Fatalf("%s: make %v, last read %v; want both", name, buf, last)
		}
		uses := append(slices.Clone(place.StaticUses[buf]), place.DecUses[buf]...)
		if len(uses) > 0 && !slices.Contains(uses, last) {
			t.Errorf("PlaceAll(%s): make %q uses = %v, want them to include %v", name, place.Cats[buf], uses, last)
		}
	}

	sum := packageSummaries(nil, ssaFuncs(pkg))[pkg.Func("toString")]
	if sum == nil || len(sum.Params) != 1 || !slices.Equal(sum.Params[0].Alias, []int{0}) {
		t.Errorf("packageSummaries(toString) = %+v, want Params[0].Alias [0]", sum)
	}
}
