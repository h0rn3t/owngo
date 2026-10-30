package census

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/ssa"
)

// TestOwnGoBuildSummary — P3.3: зведення береться з тіл цієї ssa.Program.
// Позичання відомого тіла не лишає об'єкт живим після останнього використання
// викликача. Функція без тіла і чуже зведення аналізатора об'єкт не звільняють.
func TestOwnGoBuildSummary(t *testing.T) {
	t.Cleanup(func() { PublishProgram(nil) })
	if Analyzer.FactTypes != nil {
		t.Fatal("FactTypes увімкнено, хочуть вимкнений")
	}
	lib, app := buildSummaryProgram(t)
	borrow := lib.Func("Borrow")
	if borrow == nil || borrow.Blocks == nil {
		t.Fatal("Borrow не має тіла в цій програмі")
	}
	gone := lib.Func("Gone")
	if gone == nil {
		t.Fatal("немає Gone")
	}
	gone.Blocks = nil

	useBorrow := app.Func("UseBorrow")
	useStash := app.Func("UseStash")
	useGone := app.Func("UseGone")
	allocs := map[string]ssa.Instruction{
		"UseBorrow": heapAlloc(t, useBorrow),
		"UseStash":  heapAlloc(t, useStash),
		"UseGone":   heapAlloc(t, useGone),
	}
	funcs := []*ssa.Function{useBorrow, useStash, useGone}

	// Сховище попереднього проходу аналізатора виклик відомим не робить.
	publishSummary(borrow.Object(), &Summary{Params: []Param{{}}})
	t.Cleanup(ResetSummaries)
	cats, staticUses, decUses, _, _ := Place(funcs)
	if freed, label := freedAlloc(cats, staticUses, decUses, allocs["UseBorrow"]); freed {
		t.Fatalf("Place до публікації звільнив UseBorrow (%s), хочуть живий об'єкт", label)
	}
	imported := importSummary(nil, borrow)
	if imported == nil || len(imported.Params) == 0 || !imported.Params[0].Unknown {
		t.Fatalf("importSummary(nil, Borrow) = %+v, want unknown", imported)
	}

	PublishProgram(app.Prog)
	cats, staticUses, decUses, _, _ = Place(funcs)
	if freed, label := freedAlloc(cats, staticUses, decUses, allocs["UseBorrow"]); !freed {
		t.Fatalf("Place(UseBorrow) = %s, want звільнення після останнього використання", label)
	}
	if freed, label := freedAlloc(cats, staticUses, decUses, allocs["UseStash"]); freed {
		t.Fatalf("Place(UseStash) = %s, want живий об'єкт", label)
	}
	if freed, label := freedAlloc(cats, staticUses, decUses, allocs["UseGone"]); freed {
		t.Fatalf("Place(UseGone) = %s, want живий об'єкт", label)
	}
	if Analyzer.FactTypes != nil {
		t.Fatal("PublishProgram увімкнув FactTypes")
	}
	imported = importSummary(nil, borrow)
	if imported == nil || len(imported.Params) == 0 || !imported.Params[0].Unknown {
		t.Fatalf("importSummary(nil, Borrow) після публікації = %+v, want unknown", imported)
	}
}

func freedAlloc(cats map[ssa.Instruction]string, staticUses map[ssa.Instruction][]ssa.Instruction, decUses map[ssa.Instruction][]ssa.Instruction, alloc ssa.Instruction) (bool, string) {
	label := cats[alloc]
	if label == "static" && len(staticUses[alloc]) > 0 {
		return true, label
	}
	if len(decUses[alloc]) > 0 {
		return true, label
	}
	return false, label
}

func heapAlloc(t *testing.T, fn *ssa.Function) ssa.Instruction {
	t.Helper()
	var got ssa.Instruction
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			alloc, ok := instr.(*ssa.Alloc)
			if !ok || !alloc.Heap {
				continue
			}
			if got != nil {
				t.Fatalf("%s heap allocs = more than one, want one", fn.Name())
			}
			got = instr
		}
	}
	if got == nil {
		t.Fatalf("%s heap allocs = none, want one", fn.Name())
	}
	return got
}

func buildSummaryProgram(t *testing.T) (lib, app *ssa.Package) {
	t.Helper()
	const libSrc = `package lib

var held *int

func Borrow(p *int) { _ = *p }

func Stash(p *int) { held = p }

func Gone(p *int) { _ = *p }
`
	const appSrc = `package app

import "example.com/lib"

func UseBorrow() int {
	p := new(int)
	*p = 1
	lib.Borrow(p)
	return *p
}

func UseStash() int {
	p := new(int)
	*p = 1
	lib.Stash(p)
	return *p
}

func UseGone() int {
	p := new(int)
	*p = 1
	lib.Gone(p)
	return *p
}
`
	fset := token.NewFileSet()
	parse := func(name, src string) *ast.File {
		t.Helper()
		file, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		return file
	}
	libFile := parse("lib.go", libSrc)
	appFile := parse("app.go", appSrc)
	prog := ssa.NewProgram(fset, ssa.SanityCheckFunctions)
	check := func(path, name string, file *ast.File, imp types.Importer) (*types.Package, *types.Info) {
		t.Helper()
		info := &types.Info{
			Types:      map[ast.Expr]types.TypeAndValue{},
			Defs:       map[*ast.Ident]types.Object{},
			Uses:       map[*ast.Ident]types.Object{},
			Implicits:  map[ast.Node]types.Object{},
			Scopes:     map[ast.Node]*types.Scope{},
			Selections: map[*ast.SelectorExpr]*types.Selection{},
		}
		pkg := types.NewPackage(path, name)
		if err := types.NewChecker(&types.Config{Importer: imp}, fset, pkg, info).Files([]*ast.File{file}); err != nil {
			t.Fatal(err)
		}
		return pkg, info
	}
	libTypes, libInfo := check("example.com/lib", "lib", libFile, nil)
	lib = prog.CreatePackage(libTypes, []*ast.File{libFile}, libInfo, true)
	appTypes, appInfo := check("example.com/app", "app", appFile, importerFunc(func(path string) (*types.Package, error) {
		if path == "example.com/lib" {
			return libTypes, nil
		}
		return nil, fmt.Errorf("немає пакета %s", path)
	}))
	app = prog.CreatePackage(appTypes, []*ast.File{appFile}, appInfo, true)
	prog.Build()
	return lib, app
}

type importerFunc func(path string) (*types.Package, error)

func (f importerFunc) Import(path string) (*types.Package, error) { return f(path) }
