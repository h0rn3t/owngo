//go:build owngo && owngoasan

package runtime

import (
	"unsafe"

	c "github.com/xgo-dev/llgo/runtime/internal/clite"
)

//go:linkname sanitizerAllocatedBegin C.__sanitizer_get_allocated_begin
func sanitizerAllocatedBegin(ptr c.Pointer) c.Pointer

// Base повертає початок блока malloc ASan, всередині якого лежить ptr,
// і nil для адреси поза купою ASan. Під -asan купу веде ASan (P8.3).
func Base(ptr unsafe.Pointer) unsafe.Pointer {
	return sanitizerAllocatedBegin(ptr)
}
