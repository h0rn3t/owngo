//go:build owngo

// Програма прийняття P3.1: AllocCounted/DecAccounted до нуля звільняє блок
// malloc за адресою слова, DecAccounted від zerobase нічого не звільняє.
package main

import (
	"fmt"
	"os"
	"unsafe"
)

//go:linkname allocU github.com/xgo-dev/llgo/runtime/internal/runtime.AllocU
func allocU(size uintptr) unsafe.Pointer

//go:linkname allocCounted github.com/xgo-dev/llgo/runtime/internal/runtime.AllocCounted
func allocCounted(size uintptr) unsafe.Pointer

//go:linkname inc github.com/xgo-dev/llgo/runtime/internal/runtime.Inc
func inc(ptr unsafe.Pointer)

//go:linkname decAccounted github.com/xgo-dev/llgo/runtime/internal/runtime.DecAccounted
func decAccounted(ptr unsafe.Pointer, size uintptr)

//go:linkname freed github.com/xgo-dev/llgo/runtime/internal/runtime.Freed
func freed() uint64

//go:linkname mallocSize C.malloc_size
func mallocSize(ptr unsafe.Pointer) uintptr

func main() {
	const user = 32
	ptr := allocCounted(user)
	if ptr == nil {
		fail("alloc")
	}
	word := unsafe.Sizeof(uintptr(0))
	base := unsafe.Pointer(uintptr(ptr) - word)
	if mallocSize(base) < user+word {
		fail("alloc size")
	}
	if *(*uintptr)(base) != 1 {
		fail("count")
	}

	inc(ptr)
	if *(*uintptr)(base) != 2 || mallocSize(base) == 0 {
		fail("inc")
	}
	decAccounted(ptr, user)
	if *(*uintptr)(base) != 1 || mallocSize(base) == 0 {
		fail("dec keeps")
	}

	before := freed()
	decAccounted(ptr, user)
	if mallocSize(base) != 0 {
		fail("dec free")
	}
	if freed()-before != user {
		fail("account")
	}

	base0 := allocU(0)
	if base0 == nil {
		fail("zerobase")
	}
	decAccounted(base0, user)
	if allocU(0) != base0 {
		fail("zerobase moved")
	}
	if freed()-before != user {
		fail("zerobase accounted")
	}
}

func fail(reason string) {
	fmt.Fprintln(os.Stderr, reason)
	os.Exit(1)
}
