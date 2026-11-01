//go:build owngo

// Програма прийняття P8.3: runtime.Base повертає початок блока для вказівника
// всередині блока AllocU і AllocCounted і nil для глобальної змінної, стека
// і zerobase. Без -asan додатково перевіряє сам алокатор (allocator.go).
// Код виходу 1 — неправильна база або пошкоджений блок.
package main

import (
	"os"
	"unsafe"
)

//go:linkname allocU github.com/xgo-dev/llgo/runtime/internal/runtime.AllocU
func allocU(size uintptr) unsafe.Pointer

//go:linkname allocCounted github.com/xgo-dev/llgo/runtime/internal/runtime.AllocCounted
func allocCounted(size uintptr) unsafe.Pointer

//go:linkname freeRoot github.com/xgo-dev/llgo/runtime/internal/runtime.FreeRoot
func freeRoot(ptr unsafe.Pointer)

//go:linkname base github.com/xgo-dev/llgo/runtime/internal/runtime.Base
func base(ptr unsafe.Pointer) unsafe.Pointer

//go:linkname alloca llgo.alloca
func alloca(size uintptr) unsafe.Pointer

var global [64]byte

var failed bool

func check(what string, p, want unsafe.Pointer) {
	if got := base(p); got != want {
		println(what, "FAIL: base", got, "want", want)
		failed = true
		return
	}
	println(what, "ok")
}

func main() {
	field := allocU(24)
	array := allocU(4096)
	last := allocU(100)
	large := allocU(2 << 20)
	counted := allocCounted(32)
	word := unsafe.Add(counted, -int(unsafe.Sizeof(uintptr(0))))

	check("field+8", unsafe.Add(field, 8), field)
	check("array[300]", unsafe.Add(array, 300*8), array)
	check("last-byte", unsafe.Add(last, 99), last)
	check("large+1.5MiB", unsafe.Add(large, 3<<19), large)
	check("counted+8", unsafe.Add(counted, 8), word)
	check("global", unsafe.Pointer(&global[8]), nil)
	check("stack", unsafe.Add(alloca(64), 8), nil)
	check("zerobase", allocU(0), nil)

	for _, p := range []unsafe.Pointer{field, array, last, large, word} {
		freeRoot(p)
	}
	checkAllocator()
	if failed {
		os.Exit(1)
	}
}
