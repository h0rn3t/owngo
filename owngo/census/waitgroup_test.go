package census

import (
	"go/ast"
	"go/parser"
	"go/types"
	"testing"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// TestWaitGroupGo — P7.1: WaitGroup.Go кладе f у комірку замикання,
// яке передає в go. Параметр f у зведенні втікає з причиною go,
// і так само в обгортці, що передає f у WaitGroup.Go.
func TestWaitGroupGo(t *testing.T) {
	t.Cleanup(func() { PublishProgram(nil) })
	wgGo, run := buildWaitGroupProgram(t)
	for _, fn := range []*ssa.Function{wgGo, run} {
		sum := publishedSummary(fn)
		if sum == nil || len(sum.Params) != 2 {
			t.Errorf("publishedSummary(%s) = %+v, want 2 params", fn, sum)
			continue
		}
		if got := escapeKind(sum.Params[1].Side).name(); got != "go" {
			t.Errorf("publishedSummary(%s).Params[1].Side = %q, want go", fn, got)
		}
	}
}

// buildWaitGroupProgram будує sync з тілами і пакет app з обгорткою
// app.Run(wg, f) над wg.Go(f) в одній ssa.Program і публікує її зведення.
func buildWaitGroupProgram(t *testing.T) (wgGo, run *ssa.Function) {
	t.Helper()
	pkgs, err := packages.Load(&packages.Config{Mode: packages.LoadSyntax}, "sync")
	if err != nil {
		t.Fatal(err)
	}
	if packages.PrintErrors(pkgs) > 0 {
		t.Fatal("packages.Load(sync) has errors")
	}
	prog, ssaPkgs := ssautil.Packages(pkgs, ssa.InstantiateGenerics)
	syncPkg := ssaPkgs[0]

	const src = `package app

import "sync"

func Run(wg *sync.WaitGroup, f func()) { wg.Go(f) }
`
	fset := prog.Fset
	file, err := parser.ParseFile(fset, "app.go", src, 0)
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
	imp := importerFunc(func(string) (*types.Package, error) { return syncPkg.Pkg, nil })
	tpkg := types.NewPackage("example.com/app", "app")
	if err := types.NewChecker(&types.Config{Importer: imp}, fset, tpkg, info).Files([]*ast.File{file}); err != nil {
		t.Fatal(err)
	}
	app := prog.CreatePackage(tpkg, []*ast.File{file}, info, true)
	prog.Build()
	PublishProgram(prog)

	wg := syncPkg.Pkg.Scope().Lookup("WaitGroup").Type()
	wgGo = prog.LookupMethod(types.NewPointer(wg), syncPkg.Pkg, "Go")
	if wgGo == nil || wgGo.Blocks == nil {
		t.Fatal("(*sync.WaitGroup).Go не має тіла")
	}
	return wgGo, app.Func("Run")
}
