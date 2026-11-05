//go:build llgo

package math

import (
	"unsafe"

	c "github.com/xgo-dev/llgo/runtime/internal/clite"
)

// The bit casts use a stack slot: LLGo's SSA builder puts an address-taken
// parameter on the heap.
func fmaFloat64bits(x float64) uint64 {
	p := c.Alloca(unsafe.Sizeof(x))
	*(*float64)(p) = x
	return *(*uint64)(p)
}

func fmaFloat64frombits(x uint64) float64 {
	p := c.Alloca(unsafe.Sizeof(x))
	*(*uint64)(p) = x
	return *(*float64)(p)
}
