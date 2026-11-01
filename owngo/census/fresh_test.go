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

const freshInteriorSrc = `package p

type S struct{ a, f int }

type Node struct {
	Next *Node
	Val  int
}

var (
	g  S
	gp *S
)

func field() *int {
	x := &S{a: 1}
	return &x.f
}

func bytes(n int) []byte {
	buf := make([]byte, 64)
	buf[0] = 1
	return buf[:n]
}

func nodeVal() *int {
	x := new(Node)
	return &x.Val
}

func param(p *S) *int { return &p.f }

func global() *int { return &g.f }

func stored() *int {
	x := new(S)
	gp = x
	return &x.f
}

func ring() **Node {
	x := new(Node)
	return &x.Next
}

func viaCall() *int { return field() }

func mixed(c bool) *int {
	if c {
		return &new(S).f
	}
	return new(int)
}

func shifted(i int) []byte {
	buf := make([]byte, 64)
	return buf[i:]
}

func end() []byte {
	buf := make([]byte, 64)
	return buf[64:]
}

func useField() int {
	p := field()
	return *p
}

func useBytes(n int) int {
	b := bytes(n)
	return len(b)
}

func keepField() *int {
	p := field()
	return p
}
`

// TestFreshInterior — P8.5: свіжий результат — внутрішній вказівник
// власного купового Alloc; викликач отримує на ньому Dec.
func TestFreshInterior(t *testing.T) {
	pkg := buildOnePackage(t, "example.com/p", freshInteriorSrc, ssa.SanityCheckFunctions|ssa.GlobalDebug)
	funcs := ssaFuncs(pkg)
	sums := packageSummaries(nil, funcs)
	tests := []struct {
		fn       string
		fresh    bool
		interior bool
	}{
		{"field", true, true},
		{"bytes", true, true},
		{"nodeVal", true, true},
		{"param", false, false},
		{"global", false, false},
		{"stored", false, false},
		{"ring", false, false},
		{"viaCall", false, false},
		{"mixed", false, false},
		{"shifted", false, false},
		{"end", false, false},
	}
	for _, tt := range tests {
		sum := sums[pkg.Func(tt.fn)]
		if sum.Fresh != tt.fresh || sum.Interior != tt.interior {
			t.Errorf("packageSummaries(%s) Fresh, Interior = %v, %v, want %v, %v", tt.fn, sum.Fresh, sum.Interior, tt.fresh, tt.interior)
		}
	}

	_, _, decUses, counted, _ := Place(funcs)
	for _, name := range []string{"field", "bytes", "nodeVal"} {
		for _, block := range pkg.Func(name).Blocks {
			for _, instr := range block.Instrs {
				if alloc, ok := instr.(*ssa.Alloc); ok && alloc.Heap {
					if _, ok := counted[alloc]; !ok {
						t.Errorf("Place(%s) %v not counted, want AllocCounted for a fresh interior result", name, alloc)
					}
				}
			}
		}
	}
	callers := []struct {
		fn   string
		want bool
	}{
		{"useField", true},
		{"useBytes", true},
		{"keepField", false},
	}
	for _, tt := range callers {
		call := firstCall(t, pkg.Func(tt.fn))
		if _, got := decUses[call]; got != tt.want {
			t.Errorf("Place(%s) Dec on %v = %v, want %v", tt.fn, call, got, tt.want)
		}
	}
}

func firstCall(t *testing.T, fn *ssa.Function) *ssa.Call {
	t.Helper()
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if call, ok := instr.(*ssa.Call); ok {
				return call
			}
		}
	}
	t.Fatalf("%s: no call", fn.Name())
	return nil
}
