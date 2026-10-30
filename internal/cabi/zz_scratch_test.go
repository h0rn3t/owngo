package cabi_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	gpackages "github.com/goplus/gogen/packages"
	"github.com/xgo-dev/llgo/internal/cabi"
	llssa "github.com/xgo-dev/llgo/ssa"
	"github.com/xgo-dev/llgo/ssa/ssatest"
	"golang.org/x/tools/go/ssa"

	"github.com/xgo-dev/llgo/cl"
)

func init() {
	llssa.Initialize(llssa.InitAll | llssa.InitNative)
}

func TestScratchCabiVector(t *testing.T) {
	src := "package p\n\ntype Pair struct { a, b *int }\n\nfunc Make(v *int) Pair { return Pair{v, v} }\n\nvar G = Make(nil)\n\nfunc Use() Pair { return G }\n"
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
	ret, _, err := cl.NewPackageExWithEmbedMetaOptions(llprog, nil, nil, nil, ssaPkg, []*ast.File{file}, nil, false, cl.Options{})
	if err != nil {
		t.Fatal(err)
	}
	mod := ret.Module()
	tr := cabi.NewTransformer(llprog, "x86_64-w64-windows-gnu", "", true)
	tr.LowerWindowsVectorParams(mod)
	tr.TransformModule("p", mod)
	ir := ret.String()
	nvec := 0
	for _, line := range strings.Split(ir, "\n") {
		if strings.Contains(line, "<2 x ptr>") {
			t.Log("VEC:", strings.TrimSpace(line))
			nvec++
		}
	}
	t.Logf("vector lines: %d", nvec)
}
