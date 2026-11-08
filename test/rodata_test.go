//go:build llgo
// +build llgo

package test

import (
	"testing"
	"unsafe"
)

// github.com/gofiber/fiber/v3 (readonly.go) читає межі rodata через
// go:linkname і не копіює рядок, чий вказівник лежить між ними.

//go:linkname runtimeRodata runtime.rodata
var runtimeRodata byte

//go:linkname runtimeErodata runtime.erodata
var runtimeErodata byte

func TestRuntimeRodataRangeIsEmpty(t *testing.T) {
	start := uintptr(unsafe.Pointer(&runtimeRodata))
	end := uintptr(unsafe.Pointer(&runtimeErodata))
	literal := "literal"
	heap := string([]byte{'h', 'e', 'a', 'p'})
	for _, s := range []string{literal, heap} {
		if p := uintptr(unsafe.Pointer(unsafe.StringData(s))); p >= start && p < end {
			t.Errorf("%q at %#x is inside rodata [%#x, %#x); want empty range", s, p, start, end)
		}
	}
}
