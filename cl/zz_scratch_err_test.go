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
	"github.com/xgo-dev/llvm"
	"golang.org/x/tools/go/ssa"
)

func TestScratchErrGlobalIR(t *testing.T) {
	src := "package p\n\ntype error interface { Error() string }\n\ntype errorString struct { s string }\n\nfunc (e *errorString) Error() string { return e.s }\n\nfunc New(text string) error { return &errorString{text} }\n\nvar G = New(\"hello\")\n\nfunc Use() error { return G }\n"
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
	mod := ret.Module()
	ir := ret.String()
	for _, line := range strings.Split(ir, "\n") {
		if strings.HasPrefix(line, "@") || strings.HasPrefix(line, "$") ||
			strings.Contains(line, "comdat") || strings.Contains(line, "section") ||
			strings.Contains(line, "unnamed_addr") {
			t.Log(line)
		}
	}

	// Now run llgo's own LTO pre-link pipeline (as -lto=full builds do)
	// and check where the error global lands.
	target, err := llvm.GetTargetFromTriple("x86_64-pc-windows-msvc")
	if err != nil {
		t.Fatal(err)
	}
	machine := target.CreateTargetMachineWithOptions(
		"x86_64-pc-windows-msvc", "generic", "",
		llvm.CodeGenLevelDefault, llvm.RelocPIC, llvm.CodeModelDefault,
		llvm.TargetMachineOptions{FunctionSections: true, DataSections: true, UniqueSectionNames: true},
	)
	defer machine.Dispose()
	opts := llvm.NewPassBuilderOptions()
	defer opts.Dispose()
	if err := mod.RunPasses("lto-pre-link<O2>", machine, opts); err != nil {
		t.Fatal(err)
	}
	asm, err := machine.EmitToMemoryBuffer(mod, llvm.AssemblyFile)
	if err != nil {
		t.Fatal(err)
	}
	defer asm.Dispose()
	for _, line := range strings.Split(string(asm.Bytes()), "\n") {
		if strings.Contains(line, "section") || strings.HasPrefix(line, "@") {
			t.Log("LTO-ASM:", line)
		}
	}
}
