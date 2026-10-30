package cabi_test

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xgo-dev/llgo/internal/cabi"
	llssa "github.com/xgo-dev/llgo/ssa"
	"github.com/xgo-dev/llgo/ssa/ssatest"
	"golang.org/x/tools/go/ssa"

	"github.com/xgo-dev/llgo/cl"
)

func loadStd(t *testing.T, fset *token.FileSet, path string) (*types.Package, []*ast.File, *types.Info) {
	t.Helper()
	dir := filepath.Join(os.Getenv("GOROOT"), "src", path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("no goroot %s: %v", path, err)
	}
	var files []*ast.File
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, f)
		}
	}
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Defs:  make(map[*ast.Ident]types.Object),
		Uses:  make(map[*ast.Ident]types.Object),
	}
	conf := types.Config{Importer: importer.Default()}
	pkg, err := conf.Check(path, fset, files, info)
	if err != nil {
		t.Fatal(err)
	}
	return pkg, files, info
}

func TestScratchCabiIface(t *testing.T) {
	dir := t.TempDir()
	writeFile := func(name, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeFile("go.mod", "module example.com/p\n\ngo 1.24\n")
	writeFile("p.go", "package p\n\nimport \"errors\"\n\nvar G = errors.New(\"hi\")\n\nfunc Use() error { return G }\n")
	fset := token.NewFileSet()
	cfg := &gpackages.Config{
		Fset: fset,
		Mode: gpackages.LoadSyntax,
		Dir:  dir,
		Env:  append(os.Environ(), "GO111MODULE=on", "GOTOOLCHAIN=local"),
	}
	pkgs, err := gpackages.Load(cfg, "example.com/p")
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) == 0 {
		t.Fatal("no packages")
	}

	llprog := ssatest.NewProgramEx(t, &llssa.Target{GOOS: "windows", GOARCH: "amd64"}, importer.Default())
	llprog.TypeSizes(types.SizesFor("gc", "amd64"))
	_ = errSSA
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
			if nvec > 12 {
				break
			}
		}
	}
	t.Logf("vector lines: %d", nvec)
}
