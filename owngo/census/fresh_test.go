package census

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/ssa"
)

const freshSrc = `package p

type Tree struct {
	Left, Right *Tree
}

var g *Tree

func NewTree(depth int) *Tree {
	if depth > 0 {
		return &Tree{Left: NewTree(depth - 1), Right: NewTree(depth - 1)}
	}
	return &Tree{}
}

func wrap() *Tree { return NewTree(2) }

func retParam(t *Tree) *Tree { return t }

func retGlobal() *Tree { return g }

func retPhi(t *Tree, c bool) *Tree {
	if c {
		t = new(Tree)
	}
	return t
}

func stashed() *Tree {
	t := new(Tree)
	g = t
	return t
}

func passed(f func(*Tree)) *Tree {
	t := new(Tree)
	f(t)
	return t
}

func sharedChild() *Tree {
	c := NewTree(1)
	g = c
	return &Tree{Left: c}
}
`

// TestFresh — P6.5: свіжий результат у зведенні. llgo будує SSA з
// ssa.GlobalDebug, тож DebugRef на дитині переміщення не скасовує.
func TestFresh(t *testing.T) {
	for _, mode := range []ssa.BuilderMode{ssa.SanityCheckFunctions, ssa.SanityCheckFunctions | ssa.GlobalDebug} {
		testFresh(t, mode)
	}
}

func testFresh(t *testing.T, mode ssa.BuilderMode) {
	pkg := buildOnePackage(t, "example.com/p", freshSrc, mode)
	funcs := ssaFuncs(pkg)
	sums := packageSummaries(nil, funcs)
	want := map[string]bool{
		"NewTree":     true,
		"wrap":        true,
		"retParam":    false,
		"retGlobal":   false,
		"retPhi":      false,
		"stashed":     false,
		"passed":      false,
		"sharedChild": false,
	}
	for name, fresh := range want {
		fn := pkg.Func(name)
		if got := sums[fn].Fresh; got != fresh {
			t.Errorf("mode %v: packageSummaries(%s).Fresh = %v, want %v", mode, name, got, fresh)
		}
	}
	_, _, _, counted, _ := Place(funcs)
	for _, block := range pkg.Func("NewTree").Blocks {
		for _, instr := range block.Instrs {
			if alloc, ok := instr.(*ssa.Alloc); ok && alloc.Heap {
				if _, ok := counted[alloc]; !ok {
					t.Errorf("Place(NewTree) %v not counted, want AllocCounted for a fresh result", alloc)
				}
			}
		}
	}

	// Пакет рантайму llgo свіжого результату не має: make(map) поверх makemap
	// звільняється FreeAccounted з початку блока.
	rt := buildOnePackage(t, "github.com/xgo-dev/llgo/runtime/internal/runtime", freshSrc, mode)
	rtSums := packageSummaries(nil, ssaFuncs(rt))
	if rtSums[rt.Func("NewTree")].Fresh {
		t.Error("packageSummaries(runtime NewTree).Fresh = true, want false")
	}
}

func buildOnePackage(t *testing.T, path, src string, mode ssa.BuilderMode) *ssa.Package {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "src.go", src, 0)
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
	tpkg := types.NewPackage(path, "p")
	if err := types.NewChecker(&types.Config{}, fset, tpkg, info).Files([]*ast.File{file}); err != nil {
		t.Fatal(err)
	}
	prog := ssa.NewProgram(fset, mode)
	pkg := prog.CreatePackage(tpkg, []*ast.File{file}, info, true)
	prog.Build()
	return pkg
}

func ssaFuncs(pkg *ssa.Package) []*ssa.Function {
	var funcs []*ssa.Function
	for _, mem := range pkg.Members {
		if fn, ok := mem.(*ssa.Function); ok {
			funcs = append(funcs, fn)
		}
	}
	return funcs
}
