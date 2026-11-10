package census

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/ssa"
)

// net/netip.Addr.appendTo4 під owngo: результат append, аргумент якого —
// аліас параметра через виклик (appendDecimal), той самий буфер, коли
// ємності вистачає. Census губив місце аргументу в результаті такого
// append, вважав перший буфер static і звільняв його, хоча функція його
// повертає (ASan: heap-use-after-free у net.(*TCPAddr).String).
const appendCallAliasSrc = `package p

func grow(b []byte) []byte { return append(b, 'x') }

func chain(b []byte) []byte {
	b = append(b, '.')
	b = grow(b)
	return append(b, '.')
}
`

func TestAppendThroughCallAliasKeepsSites(t *testing.T) {
	pkg := buildOnePackage(t, "example.com/p", appendCallAliasSrc, ssa.SanityCheckFunctions)
	place := PlaceAll(ssaFuncs(pkg))
	var first ssa.Instruction
	for _, b := range pkg.Func("chain").Blocks {
		for _, instr := range b.Instrs {
			if call, ok := instr.(*ssa.Call); ok && first == nil {
				if name, ok := builtinName(&call.Call); ok && name == "append" {
					first = call
				}
			}
		}
	}
	if first == nil {
		t.Fatal("chain: no append")
	}
	if cat := place.Cats[first]; strings.HasPrefix(cat, "static") {
		t.Errorf("PlaceAll(chain): first append %q, want it to escape through the returned append", cat)
	}
}
