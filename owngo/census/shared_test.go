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

func closureArgs() {
	var c Counter
	go func(d int) { c.Add(d) }(1)
}

func mixed() {
	var c Counter
	go func() { c.Add(1) }()
	f := func() { c.Add(2) }
	go f()
	f()
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
		{fn: "closureArgs", local: "c", shared: true},
		{fn: "mixed", local: "c"},
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

const sharedChanSrc = `package p

var (
	gch  chan int
	chch chan chan int
)

type box struct{ c chan int }

func worker(c chan int) { c <- 1 }

func arg() int {
	c := make(chan int)
	go worker(c)
	return <-c
}

func captured() {
	done := make(chan struct{})
	go func() { close(done) }()
	<-done
}

func selected() int {
	c := make(chan int, 1)
	go func() {
		select {
		case c <- 1:
		default:
		}
	}()
	return len(c) + cap(c)
}

func global() int {
	c := make(chan int)
	gch = c
	go worker(c)
	return <-c
}

func returned() chan int {
	c := make(chan int)
	go worker(c)
	return c
}

func sent() {
	c := make(chan int)
	go worker(c)
	chch <- c
}

func field() *box {
	c := make(chan int)
	go worker(c)
	return &box{c: c}
}

func leakInside() {
	done := make(chan struct{})
	go func() { gch2 = done }()
}

var gch2 chan struct{}
`

// TestSharedChan — P-NEW-03.5: канал з make, що йде в go аргументом, —
// місце спільної втечі; канал у захопленій комірці — її переміщений вміст.
// Канал, який ще й повертають, кладуть у глобальну змінну, поле чи інший
// канал, лишається живим.
func TestSharedChan(t *testing.T) {
	pkg := buildOnePackage(t, "example.com/p", sharedChanSrc, ssa.SanityCheckFunctions|ssa.GlobalDebug)
	var funcs []*ssa.Function
	for fn := range ssautil.AllFunctions(pkg.Prog) {
		if fn.Pkg == pkg {
			funcs = append(funcs, fn)
		}
	}
	got := PlaceAll(funcs)

	tests := []struct {
		fn      string
		arg     bool   // сам MakeChan — місце спільної втечі
		content string // тип вмісту комірки з каналом, куди йде каскад; "" — немає
	}{
		{fn: "arg", arg: true},
		{fn: "captured", content: "chan struct{}"},
		{fn: "selected", content: "chan int"},
		{fn: "global"},
		{fn: "returned"},
		{fn: "sent"},
		{fn: "field"},
		{fn: "leakInside"},
	}
	for _, tt := range tests {
		fn := pkg.Func(tt.fn)
		mc := makeChanOf(t, fn)
		goInstr := goOf(t, fn)
		_, shared := got.Shared[mc]
		if shared != tt.arg {
			t.Errorf("PlaceAll(%s).Shared[make chan] present = %v, want %v", tt.fn, shared, tt.arg)
		}
		if tt.arg {
			_, dec := got.DecUses[mc]
			if _, counted := got.Counted[mc]; !counted || !dec {
				t.Errorf("PlaceAll(%s): make chan counted %v, DecUses %v, want both", tt.fn, counted, dec)
			}
			if !slices.Contains(got.GoShared[goInstr], ssa.Value(mc)) {
				t.Errorf("PlaceAll(%s).GoShared[go] = %v, want it to contain the make chan", tt.fn, got.GoShared[goInstr])
			}
		}
		content := ""
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if alloc, ok := instr.(*ssa.Alloc); ok && alloc.Heap {
					if info := got.Shared[alloc]; info.Content != nil {
						content = info.Content.String()
					}
				}
			}
		}
		if content != tt.content {
			t.Errorf("PlaceAll(%s): cell content = %q, want %q", tt.fn, content, tt.content)
		}
	}
}

func makeChanOf(t *testing.T, fn *ssa.Function) *ssa.MakeChan {
	t.Helper()
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if mc, ok := instr.(*ssa.MakeChan); ok {
				return mc
			}
		}
	}
	t.Fatalf("%s: MakeChan not found", fn)
	return nil
}
