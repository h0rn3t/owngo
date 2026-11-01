//go:build owngo

// Програма прийняття P8.7: читання свіжого внутрішнього результату після
// Dec у викликача (P8.5) має завершитись звітом ASan. Слово знаходить
// runtime.Base, як у проході; unsafe.Pointer робить місце pinned, тож
// власного Dec прохід тут не ставить.
package main

import "unsafe"

//go:linkname base github.com/xgo-dev/llgo/runtime/internal/runtime.Base
func base(ptr unsafe.Pointer) unsafe.Pointer

//go:linkname decAccounted github.com/xgo-dev/llgo/runtime/internal/runtime.DecAccounted
func decAccounted(ptr unsafe.Pointer, size uintptr)

type S struct{ a, f int }

// field — свіжий внутрішній результат: x виділяє AllocCounted.
func field(n int) *int {
	x := &S{a: n}
	x.f = n * 2
	return &x.f
}

func main() {
	p := field(3)
	word := base(unsafe.Pointer(p))
	decAccounted(unsafe.Add(word, unsafe.Sizeof(uintptr(0))), unsafe.Sizeof(S{}))
	println(*p)
}
