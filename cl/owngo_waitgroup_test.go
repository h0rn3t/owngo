package cl

import (
	"go/ast"
	"go/parser"
	"go/types"
	"testing"

	"github.com/xgo-dev/llgo/owngo/census"
	"github.com/xgo-dev/llvm"
	"golang.org/x/tools/go/packages"
	gossa "golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// TestOwnGoWaitGroupGo — P7.1: wg.Go передає замикання в go, тож змінна,
// яку замикання захоплює, після виклику не звільняється.
func TestOwnGoWaitGroupGo(t *testing.T) {
	t.Cleanup(func() { census.PublishProgram(nil) })
	const src = `package app

import "sync"

func Spawn(wg *sync.WaitGroup) {
	x := 1
	wg.Go(func() { x++ })
}
`
	pkgs, err := packages.Load(&packages.Config{Mode: packages.LoadSyntax}, "sync")
	if err != nil {
		t.Fatal(err)
	}
	if packages.PrintErrors(pkgs) > 0 {
		t.Fatal("packages.Load(sync) has errors")
	}
	goProg, ssaPkgs := ssautil.Packages(pkgs, gossa.SanityCheckFunctions|gossa.InstantiateGenerics)
	syncTypes := ssaPkgs[0].Pkg
	file, err := parser.ParseFile(goProg.Fset, "app.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Implicits:  map[ast.Node]types.Object{},
		Scopes:     map[ast.Node]*types.Scope{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	imp := importerFunc(func(string) (*types.Package, error) { return syncTypes, nil })
	appTypes := types.NewPackage("example.com/app", "app")
	if err := types.NewChecker(&types.Config{Importer: imp}, goProg.Fset, appTypes, info).Files([]*ast.File{file}); err != nil {
		t.Fatal(err)
	}
	app := goProg.CreatePackage(appTypes, []*ast.File{file}, info, true)
	goProg.Build()
	census.PublishProgram(goProg)

	prog := newLLSSAProg(t)
	t.Cleanup(prog.Dispose)
	ret, _, err := NewPackageExWithEmbedMetaOptions(
		prog, nil, nil, nil, app, []*ast.File{file}, nil, false,
		Options{OwnGoFree: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	mod := ret.Module()
	if err := llvm.VerifyModule(mod, llvm.ReturnStatusAction); err != nil {
		t.Fatal(err)
	}
	body := mustNamedFunction(t, mod, "example.com/app.Spawn").String()
	if frees := accountedFrees(body); frees != 0 {
		t.Errorf("app.Spawn accounted frees = %d, want 0: the goroutine still reads x", frees)
	}
}
