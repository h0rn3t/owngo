package census

import (
	"testing"

	"golang.org/x/tools/go/ssa"
)

// GOROOT/test fixedbugs/issue10925.go: замикання дописує свій параметр у
// захоплену змінну. Параметр живе в комірці викликача після виклику, тож
// він не позичений.
const freeVarStoreSrc = `package p

func collect() [][]string {
	var got [][]string
	f := func(ss []string) { got = append(got, ss) }
	f([]string{"a"})
	return got
}
`

func TestClosureParamStoredIntoFreeVarEscapes(t *testing.T) {
	pkg := buildOnePackage(t, "example.com/p", freeVarStoreSrc, ssa.SanityCheckFunctions)
	closure := pkg.Func("collect").AnonFuncs[0]
	sum := packageSummaries(nil, append(ssaFuncs(pkg), closure))[closure]
	if sum == nil || len(sum.Params) != 1 {
		t.Fatalf("packageSummaries(%s) = %+v, want 1 param", closure, sum)
	}
	if p := sum.Params[0]; !p.Unknown && p.Side == 0 {
		t.Errorf("packageSummaries(%s).Params[0] = %+v, want stored (Side != 0)", closure, p)
	}
}
