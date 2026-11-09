package census

import (
	"slices"
	"testing"

	"golang.org/x/tools/go/ssa"
)

// strings.Fields під owngo: підрядки параметра лягають в елементи свіжого
// зрізу, який функція повертає, тож рядок `string(b)` у викликача живе,
// доки живе результат. До виправлення census звільняв його одразу після
// виклику, і strconv.ParseInt(f[1]) читав звільнену пам'ять (P-NEW-09.5).
const fieldsAliasSrc = `package p

func Fields(s string) []string {
	a := make([]string, 2)
	a[0] = s[:1]
	a[1] = s[1:]
	return a
}

func second(b []byte) byte {
	f := Fields(string(b))
	return f[1][0]
}
`

func TestFieldsParamAliasesResult(t *testing.T) {
	pkg := buildOnePackage(t, "example.com/p", fieldsAliasSrc, ssa.SanityCheckFunctions)
	sum := packageSummaries(nil, ssaFuncs(pkg))[pkg.Func("Fields")]
	if sum == nil || len(sum.Params) != 1 || !slices.Equal(sum.Params[0].Alias, []int{0}) {
		t.Errorf("packageSummaries(Fields) = %+v, want Params[0].Alias [0]", sum)
	}

	place := PlaceAll(ssaFuncs(pkg))
	var conv, index ssa.Instruction
	for _, b := range pkg.Func("second").Blocks {
		for _, instr := range b.Instrs {
			switch instr := instr.(type) {
			case *ssa.Convert:
				conv = instr
			case *ssa.Index:
				index = instr
			}
		}
	}
	if conv == nil || index == nil {
		t.Fatalf("second: convert %v, index %v; want both", conv, index)
	}
	uses := append(slices.Clone(place.StaticUses[conv]), place.DecUses[conv]...)
	if !slices.Contains(uses, index) {
		t.Errorf("PlaceAll(second): string(b) %q uses = %v, want them to include %v", place.Cats[conv], uses, index)
	}
}
