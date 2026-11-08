package census

import (
	"testing"

	"golang.org/x/tools/go/ssa"
)

// GOROOT/test: internal/godebug.lookup кладе new(setting) у sync.Map, а
// HashTrieMap llgo — це append літерала {key, value} у поле приймача.
// Параметр, записаний у пам'ять іншого параметра чи в глобальну змінну
// (прямо, через локальний літерал, append чи мапу), не позичається.
const paramStoreSrc = `package p

type pair struct{ p *int }
type box struct{ v pair }
type entry struct{ key, value any }
type table struct{ m []entry }

var (
	gpair pair
	gs    []pair
)

func direct(b *pair, p *int)      { b.p = p }
func viaLocal(b *box, p *int)     { b.v = pair{p: p} }
func appended(t *[]*int, p *int)  { *t = append(*t, p) }
func mapped(m map[int]*int, p *int) { m[1] = p }
func put(t *table, key, value any) { t.m = append(t.m, entry{key: key, value: value}) }
func global(p *int)               { x := pair{p: p}; gpair = x }
func globalAppend(p *int)         { gs = append(gs, pair{p: p}) }

func compare(b *pair, p *int) bool { return b.p == p }
func self(t *table)                { t.m = append(t.m, t.m...) }
`

func TestParamStoredIntoMemory(t *testing.T) {
	pkg := buildOnePackage(t, "example.com/p", paramStoreSrc, ssa.SanityCheckFunctions)
	sums := packageSummaries(nil, ssaFuncs(pkg))
	byName := map[string]*Summary{}
	for fn, sum := range sums {
		byName[fn.Name()] = sum
	}
	tests := []struct {
		fn     string
		param  int
		stored bool
	}{
		{fn: "direct", param: 1, stored: true},
		{fn: "viaLocal", param: 1, stored: true},
		{fn: "appended", param: 1, stored: true},
		{fn: "mapped", param: 1, stored: true},
		{fn: "put", param: 1, stored: true},
		{fn: "put", param: 2, stored: true},
		{fn: "global", param: 0, stored: true},
		{fn: "globalAppend", param: 0, stored: true},
		{fn: "compare", param: 1},
		{fn: "compare", param: 0},
		{fn: "self", param: 0},
	}
	for _, tt := range tests {
		sum := byName[tt.fn]
		if sum == nil || tt.param >= len(sum.Params) {
			t.Errorf("packageSummaries(%s) = %+v, want param %d", tt.fn, sum, tt.param)
			continue
		}
		p := sum.Params[tt.param]
		if got := p.Unknown || p.Side != 0; got != tt.stored {
			t.Errorf("packageSummaries(%s).Params[%d] = %+v, stored %v, want %v", tt.fn, tt.param, p, got, tt.stored)
		}
		if !tt.stored && p.Content != 0 {
			t.Errorf("packageSummaries(%s).Params[%d].Content = %d, want 0", tt.fn, tt.param, p.Content)
		}
	}
}

// reflect.(*visibleFieldsWalker).walk: місце з гілки if, записане в пам'ять
// параметра, спільне на рекурсивному виклику після гілки. Inc на виклику
// стоїть лише для значень, що домінують над ним, — інакше IR невалідний.
const incDominanceSrc = `package p

type field struct {
	typ   *node
	index []int
}

type node struct{ fields []field }

type walker struct {
	visiting map[*node]bool
	fields   []field
	index    []int
}

func walk(w *walker, t *node) {
	if w.visiting[t] {
		return
	}
	w.visiting[t] = true
	for i := range t.fields {
		f := t.fields[i]
		w.index = append(w.index, i)
		if i%2 == 0 {
			f.index = append([]int(nil), w.index...)
			w.fields = append(w.fields, f)
		}
		if f.typ != nil {
			walk(w, f.typ)
		}
		w.index = w.index[:len(w.index)-1]
	}
	delete(w.visiting, t)
}
`

func TestIncDominatesCall(t *testing.T) {
	pkg := buildOnePackage(t, "example.com/p", incDominanceSrc, ssa.SanityCheckFunctions)
	got := PlaceAll(ssaFuncs(pkg))
	for call, vals := range got.Inc {
		for _, val := range vals {
			site, ok := val.(ssa.Instruction)
			if !ok || !dominates(site, call) {
				t.Errorf("PlaceAll(walk).Inc[%v] has %v, which does not dominate the call", call, val.Name())
			}
		}
	}
}

const blankAliasSrc = `package p

type T struct{ next *T }

var sink []*T

func pass(t *T) (*T, error) { return t, nil }

func store(t *T) { sink = append(sink, t) }

func use() {
	t := new(T)
	pass(t)
	store(t)
}
`

// TestBlankAliasResult — P-NEW-09.2: виклик з кількома результатами як
// інструкція не має Extract для результату-аліаса. PlaceAll не панікує на
// ньому (стенд fiberdb під owngo).
func TestBlankAliasResult(t *testing.T) {
	pkg := buildOnePackage(t, "example.com/p", blankAliasSrc, ssa.SanityCheckFunctions)
	got := PlaceAll(ssaFuncs(pkg))
	if got.Cats == nil {
		t.Fatalf("PlaceAll(use) placed no sites")
	}
}
