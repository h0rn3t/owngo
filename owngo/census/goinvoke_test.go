package census

import (
	"testing"

	"golang.org/x/tools/go/ssa"
)

// GOROOT/test bug130.go: go i.send(c) передає горутині отримувача
// інтерфейсу так само, як аргументи. &s у коробці i мусить піти в горутину.
const goInvokeSrc = `package p

type I interface{ send(chan<- int) }

type S struct{ v int }

func (p *S) send(c chan<- int) { c <- p.v }

func start(c chan int) {
	s := S{1}
	var i I = &s
	go i.send(c)
}
`

func TestGoInvokeReceiverEscapes(t *testing.T) {
	pkg := buildOnePackage(t, "example.com/p", goInvokeSrc, ssa.SanityCheckFunctions)
	place := PlaceAll(ssaFuncs(pkg))
	var s ssa.Instruction
	for _, b := range pkg.Func("start").Blocks {
		for _, instr := range b.Instrs {
			if alloc, ok := instr.(*ssa.Alloc); ok && alloc.Comment == "s" {
				s = alloc
			}
		}
	}
	if s == nil {
		t.Fatal("start: no Alloc of s")
	}
	if got, want := place.Cats[s], "rc-shared: go"; got != want {
		t.Errorf("PlaceAll(start).Cats[s] = %q, want %q", got, want)
	}
}
