package cl

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"github.com/xgo-dev/llgo/owngo/census"
	"github.com/xgo-dev/llvm"
	gossa "golang.org/x/tools/go/ssa"
)

// TestOwnGoBuildSummary — P3.3: виклик функції з тілом у тій самій програмі
// і зведенням «позичає» звільняє об'єкт після останнього використання викликача.
// Виклик функції без тіла об'єкт не звільняє.
func TestOwnGoBuildSummary(t *testing.T) {
	t.Cleanup(func() { census.PublishProgram(nil) })
	lib, app, appFile := buildOwnGoSummaryProgram(t)
	gone := lib.Func("Gone")
	if gone == nil || gone.Blocks == nil {
		t.Fatal("Gone не має тіла до стирання")
	}
	gone.Blocks = nil
	census.PublishProgram(app.Prog)

	prog := newLLSSAProg(t)
	t.Cleanup(prog.Dispose)
	ret, _, err := NewPackageExWithEmbedMetaOptions(
		prog, nil, nil, nil, app, []*ast.File{appFile}, nil, false,
		Options{OwnGoFree: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	mod := ret.Module()
	if err := llvm.VerifyModule(mod, llvm.ReturnStatusAction); err != nil {
		t.Fatal(err)
	}
	borrow := mustNamedFunction(t, mod, "example.com/app.UseBorrow").String()
	if frees := accountedFrees(borrow); frees == 0 {
		t.Errorf("app.UseBorrow accounted frees = 0, want a free after the last use")
	}
	for _, name := range []string{"example.com/app.UseStash", "example.com/app.UseGone"} {
		body := mustNamedFunction(t, mod, name).String()
		if frees := accountedFrees(body); frees != 0 {
			t.Errorf("%s accounted frees = %d, want 0", name, frees)
		}
	}
}

func accountedFrees(ir string) int {
	return strings.Count(ir, "runtime.FreeAccounted") + strings.Count(ir, "runtime.DecAccounted")
}

// TestOwnGoStaticOutside — P4.1: місце static у пакеті поза main отримує
// FreeAccounted після PublishProgram. Невідомий виклик його не отримує.
// Місце rc-local лишається на DecAccounted. Місце static у main теж
// отримує FreeAccounted.
func TestOwnGoStaticOutside(t *testing.T) {
	t.Cleanup(func() { census.PublishProgram(nil) })
	lib, libFile, app, appFile := buildOwnGoStaticOutsideProgram(t)
	census.PublishProgram(app.Prog)

	prog := newLLSSAProg(t)
	t.Cleanup(prog.Dispose)
	compile := func(pkg *gossa.Package, file *ast.File) llvm.Module {
		t.Helper()
		ret, _, err := NewPackageExWithEmbedMetaOptions(
			prog, nil, nil, nil, pkg, []*ast.File{file}, nil, false,
			Options{OwnGoFree: true},
		)
		if err != nil {
			t.Fatal(err)
		}
		mod := ret.Module()
		if err := llvm.VerifyModule(mod, llvm.ReturnStatusAction); err != nil {
			t.Fatal(err)
		}
		return mod
	}
	libMod := compile(lib, libFile)
	appMod := compile(app, appFile)

	checks := []struct {
		mod      llvm.Module
		fn       string
		wantFree int
		wantDec  int
	}{
		{libMod, "example.com/lib.Static", 1, 0},
		{libMod, "example.com/lib.Held", 0, 0},
		// Два купові Alloc (слот і new(int)) лишаються на DecAccounted.
		// FreeAccounted тут — значення замикання без розміру кореня, не ці 8 байтів.
		{libMod, "example.com/lib.Closure", 1, 2},
		{appMod, "main.staticNew", 1, 0},
	}
	for _, check := range checks {
		body := mustNamedFunction(t, check.mod, check.fn).String()
		if got := strings.Count(body, "runtime.FreeAccounted"); got != check.wantFree {
			t.Errorf("%s runtime.FreeAccounted count = %d, want %d", check.fn, got, check.wantFree)
		}
		if got := strings.Count(body, "runtime.DecAccounted"); got != check.wantDec {
			t.Errorf("%s runtime.DecAccounted count = %d, want %d", check.fn, got, check.wantDec)
		}
		if check.fn == "example.com/lib.Closure" && !strings.Contains(body, ", i64 0, i32 0)") {
			t.Errorf("%s FreeAccounted = sized call, want only the zero-size closure value", check.fn)
		}
	}
}

func buildOwnGoStaticOutsideProgram(t *testing.T) (lib *gossa.Package, libFile *ast.File, app *gossa.Package, appFile *ast.File) {
	t.Helper()
	const libSrc = `package lib

func Static() int {
	p := new(int)
	*p = 1
	return *p
}

func Held(f func(*int)) int {
	p := new(int)
	*p = 1
	f(p)
	return *p
}

func Closure() int {
	p := new(int)
	*p = 1
	read := func() int { return *p }
	return read() + *p
}
`
	const appSrc = `package main

import "example.com/lib"

func staticNew() int {
	p := new(int)
	*p = 1
	return *p
}

func main() {
	_ = staticNew()
	_ = lib.Static()
}
`
	fset := token.NewFileSet()
	parse := func(name, src string) *ast.File {
		t.Helper()
		file, err := parser.ParseFile(fset, name, src, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		return file
	}
	libFile = parse("lib.go", libSrc)
	appFile = parse("main.go", appSrc)
	prog := gossa.NewProgram(fset, gossa.SanityCheckFunctions|gossa.InstantiateGenerics)
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
	appTypes, appInfo := check("example.com/cmd", "main", appFile, importerFunc(func(path string) (*types.Package, error) {
		if path == "example.com/lib" {
			return libTypes, nil
		}
		return nil, fmt.Errorf("missing package %s", path)
	}))
	app = prog.CreatePackage(appTypes, []*ast.File{appFile}, appInfo, true)
	prog.Build()
	return lib, libFile, app, appFile
}

func buildOwnGoSummaryProgram(t *testing.T) (lib, app *gossa.Package, appFile *ast.File) {
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
		file, err := parser.ParseFile(fset, name, src, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		return file
	}
	libFile := parse("lib.go", libSrc)
	appFile = parse("app.go", appSrc)
	prog := gossa.NewProgram(fset, gossa.SanityCheckFunctions|gossa.InstantiateGenerics)
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
		return nil, fmt.Errorf("missing package %s", path)
	}))
	app = prog.CreatePackage(appTypes, []*ast.File{appFile}, appInfo, true)
	prog.Build()
	return lib, app, appFile
}
