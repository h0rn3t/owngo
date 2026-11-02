package census

import (
	"testing"

	"golang.org/x/tools/go/ssa"
)

// GOROOT/test cmp.go, method5.go, bug514.go: значення типу, що llgo кладе в
// слово даних інтерфейсу без копії (ssa/abitype.go, directIfaceType), — не
// купова алокація. Звільнення слова даних звільняло б канал, мапу чи
// глобальну змінну, на яку воно вказує.
const directIfaceSrc = `package p

import "unsafe"

type ptr *int
type one struct{ p *int }
type nested struct{ o one }
type arr [1]*int
type ch struct{ c chan int }
type two struct{ a, b *int }
type pair [2]*int

var g int

func conv(c chan int, m map[int]int, f func(), u unsafe.Pointer, n int, s string) []any {
	return []any{
		ptr(&g),
		one{&g},
		nested{one{&g}},
		arr{&g},
		ch{c},
		c,
		m,
		f,
		u,
		two{&g, &g},
		pair{&g, &g},
		n,
		s,
	}
}
`

func TestDirectInterfaceNotSite(t *testing.T) {
	pkg := buildOnePackage(t, "example.com/p", directIfaceSrc, ssa.SanityCheckFunctions)
	want := map[string]bool{
		"example.com/p.ptr":    false,
		"example.com/p.one":    false,
		"example.com/p.nested": false,
		"example.com/p.arr":    false,
		"example.com/p.ch":     false,
		"chan int":             false,
		"map[int]int":          false,
		"func()":               false,
		"unsafe.Pointer":       false,
		"example.com/p.two":    true,
		"example.com/p.pair":   true,
		"int":                  true,
		"string":               true,
	}
	seen := map[string]bool{}
	for _, fn := range ssaFuncs(pkg) {
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				mi, ok := instr.(*ssa.MakeInterface)
				if !ok {
					continue
				}
				name := mi.X.Type().String()
				site, known := want[name]
				if !known {
					continue
				}
				seen[name] = true
				if got := isSite(mi); got != site {
					t.Errorf("isSite(MakeInterface %s) = %v, want %v", name, got, site)
				}
				if got := seed(mi, func(ssa.Value) siteSet { return siteSet{} }); got != site {
					t.Errorf("seed(MakeInterface %s) = %v, want %v", name, got, site)
				}
			}
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("fixture has no MakeInterface of %s", name)
		}
	}
}

// Значення, дістане з інтерфейсу, — аліас його даних: len(im.(map[int]int))
// читає ту саму мапу, тож Dec мапи стоїть після цього читання.
const typeAssertSrc = `package p

func plain() int {
	m := map[int]int{1: 2}
	var im any = m
	return len(im.(map[int]int))
}

func commaOk() int {
	m := map[int]int{1: 2}
	var im any = m
	n, _ := im.(map[int]int)
	return len(n)
}

func toIface() int {
	m := map[int]int{1: 2}
	var im any = m
	s := im.(interface{})
	return len(s.(map[int]int))
}
`

func TestTypeAssertAliasesData(t *testing.T) {
	pkg := buildOnePackage(t, "example.com/p", typeAssertSrc, ssa.SanityCheckFunctions)
	for _, fn := range ssaFuncs(pkg) {
		var instrs []ssa.Instruction
		var site ssa.Instruction
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				instrs = append(instrs, instr)
				if _, ok := instr.(*ssa.MakeMap); ok {
					site = instr
				}
			}
		}
		if site == nil {
			continue
		}
		pts, _ := pointTo(instrs, nil)
		for _, instr := range instrs {
			call, ok := instr.(*ssa.Call)
			if !ok {
				continue
			}
			if b, ok := call.Call.Value.(*ssa.Builtin); !ok || b.Name() != "len" {
				continue
			}
			arg := call.Call.Args[0]
			if _, ok := pts[arg][site]; !ok {
				t.Errorf("pointTo(%s)[%s] = %v, want the MakeMap site", fn.Name(), arg.Name(), pts[arg])
			}
		}
	}
}
