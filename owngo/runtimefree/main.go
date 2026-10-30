//go:build owngo

// Програма прийняття P1.2: AllocU/Free звільняє блок malloc, Free від
// zerobase лишає спільну базу на місці.
package main

import (
	"fmt"
	"os"
	"unsafe"
)

//go:linkname allocU github.com/xgo-dev/llgo/runtime/internal/runtime.AllocU
func allocU(size uintptr) unsafe.Pointer

//go:linkname free github.com/xgo-dev/llgo/runtime/internal/runtime.Free
func free(ptr unsafe.Pointer)

//go:linkname mallocSize C.malloc_size
func mallocSize(ptr unsafe.Pointer) uintptr

func main() {
	block := allocU(32)
	if block == nil || mallocSize(block) == 0 {
		fail("alloc")
	}
	free(block)
	if mallocSize(block) != 0 {
		fail("free")
	}

	base := allocU(0)
	if base == nil {
		fail("zerobase")
	}
	free(base)
	if allocU(0) != base {
		fail("zerobase moved")
	}
}

func fail(reason string) {
	fmt.Fprintln(os.Stderr, reason)
	os.Exit(1)
}
