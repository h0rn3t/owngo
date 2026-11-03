package census

import (
	"go/token"
	"slices"
	"testing"

	"golang.org/x/tools/go/ssa"
)

// GOROOT/test fixedbugs/dse_move_auxint.go: [8]byte(dst) читає масив зрізу
// через SliceToArrayPointer. Буфер dst живе до цього читання.
const sliceToArraySrc = `package p

func f(n int) [8]byte {
	dst := make([]byte, n)
	dst[0] = 99
	return [8]byte(dst)
}
`

func TestSliceToArrayReadsSlice(t *testing.T) {
	pkg := buildOnePackage(t, "example.com/p", sliceToArraySrc, ssa.SanityCheckFunctions)
	place := PlaceAll(ssaFuncs(pkg))
	var buf, read ssa.Instruction
	for _, b := range pkg.Func("f").Blocks {
		for _, instr := range b.Instrs {
			switch instr := instr.(type) {
			case *ssa.MakeSlice:
				buf = instr
			case *ssa.UnOp:
				if instr.Op == token.MUL {
					read = instr
				}
			}
		}
	}
	if buf == nil || read == nil {
		t.Fatalf("f: make %v, array read %v; want both", buf, read)
	}
	uses := append(slices.Clone(place.StaticUses[buf]), place.DecUses[buf]...)
	if len(uses) > 0 && !slices.Contains(uses, read) {
		t.Errorf("PlaceAll(f): make %q uses = %v, want them to include %v", place.Cats[buf], uses, read)
	}
}
