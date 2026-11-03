package census

import (
	"testing"

	"golang.org/x/tools/go/ssa"
)

// GOROOT/test convT2X.go: значення, надіслане в канал гілкою select, іде з
// функції так само, як надіслане c <- v. Без цього коробку інтерфейсу
// звільняло до того, як отримувач її прочитав.
const selectSendSrc = `package p

type T struct{ n int }

func local(c chan *T) {
	select {
	case c <- &T{1}:
	default:
	}
}

func param(c chan *T, p *T) {
	select {
	case c <- p:
	default:
	}
}
`

func TestSelectSendEscapesToChannel(t *testing.T) {
	pkg := buildOnePackage(t, "example.com/p", selectSendSrc, ssa.SanityCheckFunctions)
	place := PlaceAll(ssaFuncs(pkg))
	var box ssa.Instruction
	for _, b := range pkg.Func("local").Blocks {
		for _, instr := range b.Instrs {
			if alloc, ok := instr.(*ssa.Alloc); ok && alloc.Heap {
				box = alloc
			}
		}
	}
	if box == nil {
		t.Fatal("local: no heap Alloc of &T{1}")
	}
	if got, want := place.Cats[box], "rc-shared: channel"; got != want {
		t.Errorf("PlaceAll(local).Cats[&T{1}] = %q, want %q", got, want)
	}

	sums := packageSummaries(nil, ssaFuncs(pkg))
	sum := sums[pkg.Func("param")]
	if sum == nil || len(sum.Params) != 2 {
		t.Fatalf("packageSummaries(param) = %+v, want 2 params", sum)
	}
	if got := escapeKind(sum.Params[1].Side); got != escapeChannel {
		t.Errorf("packageSummaries(param).Params[1].Side = %v, want %v", got.name(), escapeChannel.name())
	}
}
