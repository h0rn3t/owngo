package census

import (
	"slices"
	"testing"

	"golang.org/x/tools/go/ssa"
)

// GOROOT/test issue31546.go: обгортка (*Value).Field має той самий
// types.Object, що й (Value).Field. PublishProgram писав обидва зведення під
// один ключ, і викликач (Value).Field випадково отримував зведення обгортки
// без Alias: коробку reflect.ValueOf звільняло до Int.
const publishKeySrc = `package p

type V struct{ p *int }

func (v V) Ptr() *int { return v.p }

var sink interface{ Ptr() *int } = &V{}
`

func TestPublishedSummaryIgnoresWrapper(t *testing.T) {
	pkg := buildOnePackage(t, "example.com/p", publishKeySrc, ssa.SanityCheckFunctions)
	method := pkg.Prog.LookupMethod(pkg.Type("V").Type(), pkg.Pkg, "Ptr")
	// Порядок функцій у PublishProgram залежить від ітерації мапи: кілька
	// прогонів дають обидва порядки.
	for range 20 {
		PublishProgram(pkg.Prog)
		got := publishedSummary(method)
		if got == nil || len(got.Params) != 1 || !slices.Equal(got.Params[0].Alias, []int{0}) {
			t.Fatalf("publishedSummary(%s) = %+v, want Params[0].Alias [0]", method, got)
		}
	}
}
