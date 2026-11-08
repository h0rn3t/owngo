package census

import (
	"slices"
	"testing"

	"golang.org/x/tools/go/ssa"
)

// GOROOT/test: os/exec.(*Cmd).writerDescriptor кладе замикання, що захопило
// параметр w, через append у поле приймача. Комірка w мусить пережити
// функцію, як і коли замикання повернуто: Dec на ній звільняв w до виклику
// замикання з горутини.
const closureLeaveSrc = `package p

type cmd struct {
	f    func() int
	jobs []func() int
	p    *int
}

func returned(w []int) func() int { return func() int { return len(w) } }
func field(c *cmd, w []int)       { c.f = func() int { return len(w) } }
func appended(c *cmd, w []int)    { c.jobs = append(c.jobs, func() int { return len(w) }) }
func keep(c *cmd, f func() int)   { c.jobs = append(c.jobs, f) }
func moved(c *cmd, w []int)       { keep(c, func() int { return len(w) }) }
func local(w []int) int           { f := func() int { return len(w) }; return f() }
func stored(c *cmd)               { c.p = new(int) }
`

func TestClosureLeavesWithCapturedCell(t *testing.T) {
	pkg := buildOnePackage(t, "example.com/p", closureLeaveSrc, ssa.SanityCheckFunctions)
	place := PlaceAll(ssaFuncs(pkg))
	cell := map[string]ssa.Instruction{}
	var stored ssa.Instruction
	for _, fn := range ssaFuncs(pkg) {
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				alloc, ok := instr.(*ssa.Alloc)
				if !ok {
					continue
				}
				if alloc.Comment == "w" {
					cell[fn.Name()] = alloc
				}
				if fn.Name() == "stored" && alloc.Heap {
					stored = alloc
				}
			}
		}
	}
	tests := []struct {
		fn  string
		dec bool
	}{
		{fn: "returned"},
		{fn: "field"},
		{fn: "appended"},
		{fn: "moved"},
		{fn: "local", dec: true},
	}
	for _, tt := range tests {
		site := cell[tt.fn]
		if site == nil {
			t.Errorf("%s: no Alloc of w", tt.fn)
			continue
		}
		if _, got := place.DecUses[site]; got != tt.dec {
			t.Errorf("PlaceAll(%s).DecUses[w] present = %v, want %v (category %q)", tt.fn, got, tt.dec, place.Cats[site])
		}
	}
	if stored == nil {
		t.Fatal("stored: no Alloc of new(int)")
	}
	if got := place.Cats[stored]; got != "rc-local: call" {
		t.Errorf("PlaceAll(stored).Cats[new(int)] = %q, want %q", got, "rc-local: call")
	}
}

// GOROOT/test escape.go for_escapes3: замикання лежить у локальному масиві
// і викликається через f[0](). Dec комірки p стоїть після цього виклику, а не
// після запису замикання в масив.
const closureArraySrc = `package p

func stored(x int) int {
	var f [1]func() int
	p := new(int)
	*p = x
	f[0] = func() int { return *p }
	return f[0]()
}
`

func TestClosureInLocalArrayKeepsCell(t *testing.T) {
	pkg := buildOnePackage(t, "example.com/p", closureArraySrc, ssa.SanityCheckFunctions)
	place := PlaceAll(ssaFuncs(pkg))
	fn := pkg.Func("stored")
	var cell, call ssa.Instruction
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			switch instr := instr.(type) {
			case *ssa.Alloc:
				if instr.Comment == "p" {
					cell = instr
				}
			case *ssa.Call:
				if instr.Call.StaticCallee() == nil {
					call = instr
				}
			}
		}
	}
	if cell == nil || call == nil {
		t.Fatalf("stored: cell %v, closure call %v; want both", cell, call)
	}
	uses, ok := place.DecUses[cell]
	if ok && !slices.Contains(uses, call) {
		t.Errorf("PlaceAll(stored).DecUses[p] = %v, want it to include %v", uses, call)
	}
}

// P-NEW-09.2, os/signal.Notify: комірку c захоплює getHandler, а getHandler —
// add. Dec комірки c стоїть після виклику add, а не після запису getHandler у
// його комірку: інакше add читає звільнену c (ASan на стенді fiberdb).
const nestedClosureSrc = `package p

var table = map[chan int]*int{}

func notify(c chan int, sig ...int) {
	get := func() *int {
		h := table[c]
		if h == nil {
			h = new(int)
			table[c] = h
		}
		return h
	}
	add := func(n int) {
		*get() += n
	}
	for _, s := range sig {
		add(s)
	}
}
`

func TestNestedClosureKeepsCell(t *testing.T) {
	pkg := buildOnePackage(t, "example.com/p", nestedClosureSrc, ssa.SanityCheckFunctions)
	place := PlaceAll(ssaFuncs(pkg))
	fn := pkg.Func("notify")
	var cell, call ssa.Instruction
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			switch instr := instr.(type) {
			case *ssa.Alloc:
				if instr.Comment == "c" {
					cell = instr
				}
			case *ssa.Call:
				if _, closure := instr.Call.Value.(*ssa.MakeClosure); closure {
					call = instr
				}
			}
		}
	}
	if cell == nil || call == nil {
		t.Fatalf("notify: cell %v, closure call %v; want both", cell, call)
	}
	uses, ok := place.DecUses[cell]
	if ok && !slices.Contains(uses, call) {
		t.Errorf("PlaceAll(notify).DecUses[c] = %v, want it to include %v", uses, call)
	}
}
