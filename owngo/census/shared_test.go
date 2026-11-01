package census

import (
	"slices"
	"testing"

	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

const sharedSrc = `package p

type Tree struct{ Left, Right *Tree }

func NewTree(depth int) *Tree {
	if depth > 0 {
		return &Tree{Left: NewTree(depth - 1), Right: NewTree(depth - 1)}
	}
	return &Tree{}
}

func (t *Tree) Count() int {
	if t.Left == nil {
		return 1
	}
	return 1 + t.Left.Count() + t.Right.Count()
}

type Counter struct{ n int }

func (c *Counter) Add(d int) { c.n += d }

var (
	sink  *Counter
	gtree *Tree
	ch    chan *Counter
)

func capture() int {
	var c Counter
	go func() { c.Add(1) }()
	c.Add(2)
	return c.n
}

func tree() int {
	var t *Tree
	go func() { t = NewTree(3) }()
	return t.Count()
}

func arg() {
	c := new(Counter)
	go bump(c)
}

func bump(c *Counter) { c.Add(1) }

func global() {
	var c Counter
	sink = &c
	go func() { c.Add(1) }()
}

func channel() {
	var c Counter
	ch <- &c
	go func() { c.Add(1) }()
}

func leak() {
	var c Counter
	go func() { sink = &c }()
}

func treeLeak() {
	var t *Tree
	go func() { t = NewTree(1) }()
	gtree = t
}

func treeParam(p *Tree) int {
	var t *Tree
	go func() { t = p }()
	return t.Count()
}

func treeGoLeak() {
	var t *Tree
	go func() {
		t = NewTree(1)
		gtree = t
	}()
}
`

// TestSharedGo — P9.3: купове місце, яке йде в go зв'язкою замикання чи
// аргументом, а інших втеч, крім дозволених rc-local, не має, отримує
// атомарне слово, IncShared перед go і DecShared у функції й у горутині.
// Каскад іде у вміст комірки лише за правилом переміщення.
func TestSharedGo(t *testing.T) {
	pkg := buildOnePackage(t, "example.com/p", sharedSrc, ssa.SanityCheckFunctions|ssa.GlobalDebug)
	var funcs []*ssa.Function
	for fn := range ssautil.AllFunctions(pkg.Prog) {
		if fn.Pkg == pkg {
			funcs = append(funcs, fn)
		}
	}
	got := PlaceAll(funcs)

	tests := []struct {
		fn, local   string
		shared      bool
		contentElem string // тип об'єкта у вмісті, куди йде каскад; "" — каскаду немає
	}{
		{fn: "capture", local: "c", shared: true},
		{fn: "tree", local: "t", shared: true, contentElem: "example.com/p.Tree"},
		{fn: "arg", local: "", shared: true},
		{fn: "global", local: "c"},
		{fn: "channel", local: "c"},
		{fn: "leak", local: "c"},
		{fn: "treeLeak", local: "t", shared: true},
		{fn: "treeGoLeak", local: "t", shared: true},
		{fn: "treeParam", local: "t", shared: true},
	}
	for _, tt := range tests {
		fn := pkg.Func(tt.fn)
		site := localAlloc(t, fn, tt.local)
		goInstr := goOf(t, fn)
		info, shared := got.Shared[site]
		if shared != tt.shared {
			t.Errorf("PlaceAll(%s).Shared[%s] present = %v, want %v", tt.fn, site.Comment, shared, tt.shared)
			continue
		}
		_, counted := got.Counted[site]
		_, dec := got.DecUses[site]
		goVals := got.GoShared[goInstr]
		if !tt.shared {
			if counted || dec || len(goVals) != 0 {
				t.Errorf("PlaceAll(%s): counted %v, DecUses %v, GoShared %v, want none", tt.fn, counted, dec, goVals)
			}
			continue
		}
		if !counted || !dec {
			t.Errorf("PlaceAll(%s): counted %v, DecUses %v, want both", tt.fn, counted, dec)
		}
		if !slices.Contains(goVals, ssa.Value(site)) {
			t.Errorf("PlaceAll(%s).GoShared[go] = %v, want it to contain %v", tt.fn, goVals, site.Name())
		}
		elem := ""
		if info.Content != nil {
			elem = info.Content.String()
		}
		if elem != tt.contentElem {
			t.Errorf("PlaceAll(%s).Shared[%s].Content = %q, want %q", tt.fn, site.Comment, elem, tt.contentElem)
		}
	}

	// Запис NewTree у захоплену змінну — переміщення, без Inc.
	for _, anon := range pkg.Func("tree").AnonFuncs {
		for _, block := range anon.Blocks {
			for _, instr := range block.Instrs {
				if vals := got.Inc[instr]; len(vals) != 0 {
					t.Errorf("PlaceAll(tree).Inc[%v] = %v, want none: the fresh tree moves into the cell", instr, vals)
				}
			}
		}
	}
}

// localAlloc повертає купове місце функції fn зі змінною local (порожнє
// ім'я — перше купове місце).
func localAlloc(t *testing.T, fn *ssa.Function, local string) *ssa.Alloc {
	t.Helper()
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if alloc, ok := instr.(*ssa.Alloc); ok && alloc.Heap && (local == "" || alloc.Comment == local) {
				return alloc
			}
		}
	}
	t.Fatalf("%s: heap Alloc %q not found", fn, local)
	return nil
}

func goOf(t *testing.T, fn *ssa.Function) *ssa.Go {
	t.Helper()
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if g, ok := instr.(*ssa.Go); ok {
				return g
			}
		}
	}
	t.Fatalf("%s: go not found", fn)
	return nil
}
