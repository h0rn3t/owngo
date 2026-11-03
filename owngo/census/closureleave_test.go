package census

import (
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
