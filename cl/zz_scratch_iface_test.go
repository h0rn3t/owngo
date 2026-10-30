package cl

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	gpackages "github.com/goplus/gogen/packages"
	llssa "github.com/xgo-dev/llgo/ssa"
	"github.com/xgo-dev/llgo/ssa/ssatest"
	"golang.org/x/tools/go/ssa"
)

func TestScratchIfaceSret(t *testing.T) {
	src := "package p\n\nfunc New(text string) any { return &text }\n\nvar G any = New(\"hello\")\n\nfunc Use() any { return G }\n"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Defs:  make(map[*ast.Ident]types.Object),
		Uses:  make(map[*ast.Ident]types.Object),
	}
	conf := types.Config{Importer: gpackages.NewImporter(fset)}
	pkg, err := conf.Check("p", fset, []*ast.File{file}, info)
	if err != nil {
		t.Fatal(err)
	}
	prog := ssa.NewProgram(fset, ssa.SanityCheckFunctions|ssa.InstantiateGenerics)
	ssaPkg := prog.CreatePackage(pkg, []*ast.File{file}, info, true)
	ssaPkg.Build()

	llprog := ssatest.NewProgramEx(t, &llssa.Target{GOOS: "windows", GOARCH: "amd64"}, conf.Importer)
	llprog.TypeSizes(types.SizesFor("gc", "amd64"))
	ret, _, err := NewPackageExWithEmbedMetaOptions(llprog, nil, nil, nil, ssaPkg, []*ast.File{file}, nil, false, Options{})
	if err != nil {
		t.Fatal(err)
	}
	ir := ret.String()
	for _, line := range strings.Split(ir, "\n") {
		if strings.HasPrefix(line, "@") && strings.Contains(line, "global") {
			t.Log(line)
		}
		if strings.Contains(line, "<2 x ptr>") {
			t.Log("VEC:", strings.TrimSpace(line))
		}
	}
}
