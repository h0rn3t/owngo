package missing

import (
	"testing"
	_ "unsafe"
)

//go:linkname undefinedSymbol C.llgo_linkfail_undefined_symbol
func undefinedSymbol()

func TestMissing(t *testing.T) {
	undefinedSymbol()
}
