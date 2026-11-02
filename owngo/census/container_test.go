package census

import (
	"testing"

	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// GOROOT/test map.go: місце, що лежить у мапі, і сама мапа живі, доки з неї
// читають. Читання через Lookup, Range і Next — вживання місця.
const containerSrc = `package p

func lookup() string {
	m := map[string][]string{}
	as := make([]string, 2)
	as[0] = "x"
	m["a"] = as
	return m["a"][0]
}

func commaOk() string {
	m := map[string][]string{}
	as := make([]string, 2)
	as[0] = "x"
	m["a"] = as
	v, _ := m["a"]
	return v[0]
}

func rangeMap() int {
	m := map[int]int{1: 1}
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

func rangeValue() string {
	m := map[string][]string{}
	as := make([]string, 2)
	as[0] = "x"
	m["a"] = as
	s := ""
	for _, v := range m {
		s += v[0]
	}
	return s
}
`

func TestContainerReadsAreUses(t *testing.T) {
	pkg := buildOnePackage(t, "example.com/p", containerSrc, ssa.SanityCheckFunctions)
	var funcs []*ssa.Function
	for fn := range ssautil.AllFunctions(pkg.Prog) {
		if fn.Pkg == pkg {
			funcs = append(funcs, fn)
		}
	}
	got := PlaceAll(funcs)

	alloc := func(fn *ssa.Function) ssa.Instruction {
		allocs := heapAllocs(fn)
		if len(allocs) != 1 {
			t.Fatalf("%s: %d heap Allocs, want 1", fn.Name(), len(allocs))
		}
		return allocs[0]
	}
	makeMap := func(fn *ssa.Function) ssa.Instruction {
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				if m, ok := instr.(*ssa.MakeMap); ok {
					return m
				}
			}
		}
		t.Fatalf("%s: no MakeMap", fn.Name())
		return nil
	}
	// Вживання — сама інструкція чи будь-яка, що бере її результат операндом.
	through := func(is func(ssa.Value) bool) func(ssa.Instruction) bool {
		return func(i ssa.Instruction) bool {
			if v, ok := i.(ssa.Value); ok && is(v) {
				return true
			}
			for _, op := range i.Operands(nil) {
				if *op != nil && is(*op) {
					return true
				}
			}
			return false
		}
	}
	isLookup := through(func(v ssa.Value) bool { _, ok := v.(*ssa.Lookup); return ok })
	isNext := through(func(v ssa.Value) bool { _, ok := v.(*ssa.Next); return ok })
	tests := []struct {
		name string
		site func(*ssa.Function) ssa.Instruction
		want func(ssa.Instruction) bool
	}{
		{"lookup", alloc, isLookup},
		{"commaOk", alloc, isLookup},
		{"rangeMap", makeMap, isNext},
		{"rangeValue", alloc, isNext},
		{"rangeValue", makeMap, isNext},
	}
	for _, tt := range tests {
		site := tt.site(pkg.Func(tt.name))
		uses, static := got.StaticUses[site]
		if !static {
			continue // місце не звільняється — читання після MapUpdate безпечне
		}
		found := false
		for _, u := range uses {
			found = found || tt.want(u)
		}
		if !found {
			t.Errorf("PlaceAll(%s).StaticUses[%v] = %v, want the read through the map", tt.name, site, uses)
		}
	}
}
