package ssa

import (
	"go/types"
	"strings"
	"testing"

	"github.com/xgo-dev/llvm"
)

func TestUnixODRDefinitionsDoNotGainCOMDAT(t *testing.T) {
	prog := NewProgram(&Target{GOOS: "linux", GOARCH: "amd64"})
	defer prog.Dispose()
	pkg := prog.NewPackage("example.com/p", "example.com/p")

	global := pkg.NewVar("shared.global", types.NewPointer(types.Typ[types.Int]), GoDecl)
	global.Init(prog.IntVal(0, prog.Int()))
	pkg.setODRLinkage(global.impl, llvm.WeakODRLinkage)

	if ir := pkg.String(); strings.Contains(ir, "comdat") {
		t.Fatalf("non-Windows ODR IR unexpectedly contains COMDAT:\n%s", ir)
	}
}
