package cl

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"strings"
	"testing"

	"github.com/goplus/gogen/packages"
	"github.com/xgo-dev/llgo/internal/cabi"
	llssa "github.com/xgo-dev/llgo/ssa"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

func TestScratchVecSrc(t *testing.T) {
	src := "package p\n\ntype Iface interface { M() }\n\nvar A Iface\n\nvar G = A\n\nfunc Use() Iface { return G }\n"
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "foo.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	files := []*ast.File{f}
	pkg := types.NewPackage("p", "p")
	imp := packages.NewImporter(fset)
	ssaPkg, _, err := ssautil.BuildPackage(&types.Config{Importer: imp}, fset, pkg, files, ssa.SanityCheckFunctions|ssa.InstantiateGenerics)
	if err != nil {
		t.Fatal(err)
	}
	prog := llssa.NewProgram(&llssa.Target{GOOS: "windows", GOARCH: "amd64"})
	prog.SetRuntime(func() *types.Package {
		rt, err := importer.For("source", nil).Import(llssa.PkgRuntime)
		if err != nil {
			t.Fatal("load runtime failed:", err)
		}
		return rt
	})
	prog.TypeSizes(types.SizesFor("gc", "amd64"))
	ret, err := NewPackage(prog, ssaPkg, files)
	if err != nil {
		t.Fatal(err)
	}
	mod := ret.Module()
	tr := cabi.NewTransformer(prog, "x86_64-w64-windows-gnu", "", false)
	tr.LowerWindowsVectorParams(mod)
	tr.TransformModule("p", mod)
	ir := ret.String()
	if out := os.Getenv("LLGO_SCRATCH_DUMP"); out != "" {
		os.WriteFile(out, []byte(ir), 0o644)
	}
	n := 0
	for _, line := range strings.Split(ir, "\n") {
		if strings.Contains(line, "<2 x ptr>") {
			t.Log("VEC:", strings.TrimSpace(line))
			n++
			if n > 8 {
				break
			}
		}
	}
	t.Logf("vector lines: %d", n)
}
