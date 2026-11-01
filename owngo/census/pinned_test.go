package census

import (
	"testing"

	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

const pinnedSrc = `package p

import "unsafe"

type T struct{ a, b int }

type Node struct {
	next *Node
	data *T
}

var (
	addr uintptr
	keep unsafe.Pointer
)

func New() *T { return &T{} }

func callerPins() {
	p := New()
	addr = uintptr(unsafe.Pointer(p))
}

func stash(p *T) { addr = uintptr(unsafe.Pointer(p)) }

func calleePins() int {
	p := &T{}
	stash(p)
	return p.a
}

func hold(p *T) { keep = unsafe.Pointer(p) }

func calleeKeeps() int {
	p := &T{}
	hold(p)
	return p.a
}

func fieldPins() int {
	a := &Node{}
	b := &Node{}
	a.next = b
	b.next = a
	t := &T{}
	b.data = t
	keep = unsafe.Pointer(b)
	return t.a
}
`

// TestPinnedAcrossCalls — P10.3: вказівник, що стає unsafe.Pointer не у
// функції місця, закріплює місце. Ні Dec, ні FreeAccounted, ні слова.
func TestPinnedAcrossCalls(t *testing.T) {
	pkg := buildOnePackage(t, "example.com/p", pinnedSrc, ssa.SanityCheckFunctions|ssa.GlobalDebug)
	var funcs []*ssa.Function
	for fn := range ssautil.AllFunctions(pkg.Prog) {
		if fn.Pkg == pkg {
			funcs = append(funcs, fn)
		}
	}
	got := PlaceAll(funcs)

	tests := []struct {
		name string
		site ssa.Instruction
	}{
		{name: "callerPins", site: callOf(t, pkg.Func("callerPins"))},
		{name: "calleePins", site: localAlloc(t, pkg.Func("calleePins"), "")},
		{name: "calleeKeeps", site: localAlloc(t, pkg.Func("calleeKeeps"), "")},
	}
	fieldAllocs := heapAllocs(pkg.Func("fieldPins"))
	if len(fieldAllocs) != 3 {
		t.Fatalf("fieldPins: %d heap Allocs, want 3", len(fieldAllocs))
	}
	for i, alloc := range fieldAllocs {
		tests = append(tests, struct {
			name string
			site ssa.Instruction
		}{name: "fieldPins" + string(rune('A'+i)), site: alloc})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			label := got.Cats[tt.site]
			_, counted := got.Counted[tt.site]
			_, dec := got.DecUses[tt.site]
			_, free := got.StaticUses[tt.site]
			if counted || dec || free {
				t.Errorf("PlaceAll(%s) site %v (%q): counted %v, DecUses %v, StaticUses %v, want none", tt.name, tt.site, label, counted, dec, free)
			}
		})
	}
	for _, tt := range tests[:3] {
		if label := got.Cats[tt.site]; label != "pinned: unsafe" {
			t.Errorf("PlaceAll(%s).Cats[%v] = %q, want %q", tt.name, tt.site, label, "pinned: unsafe")
		}
	}
}

func callOf(t *testing.T, fn *ssa.Function) *ssa.Call {
	t.Helper()
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if call, ok := instr.(*ssa.Call); ok {
				return call
			}
		}
	}
	t.Fatalf("%s: call not found", fn)
	return nil
}

func heapAllocs(fn *ssa.Function) []*ssa.Alloc {
	var out []*ssa.Alloc
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if alloc, ok := instr.(*ssa.Alloc); ok && alloc.Heap {
				out = append(out, alloc)
			}
		}
	}
	return out
}
