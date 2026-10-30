package cl

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"github.com/goplus/gogen/packages"
	llssa "github.com/xgo-dev/llgo/ssa"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

func TestScratchVecSrc(t *testing.T) {
	src := "package p\n\nfunc New() any { return 42 }\n\nvar G any = New()\n\nfunc Use() any { return G }\n"
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
	prog := llssa.NewProgram(nil)
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
	ir := ret.String()
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
