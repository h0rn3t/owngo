//go:build owngo

// Програма прийняття P8.7: читання поля через похідний вказівник після Dec
// місця P8.4 має завершитись звітом ASan.
package main

import "unsafe"

//go:linkname allocCounted github.com/xgo-dev/llgo/runtime/internal/runtime.AllocCounted
func allocCounted(size uintptr) unsafe.Pointer

//go:linkname decAccounted github.com/xgo-dev/llgo/runtime/internal/runtime.DecAccounted
func decAccounted(ptr unsafe.Pointer, size uintptr)

type S struct{ a, f int }

func main() {
	n := unsafe.Sizeof(S{})
	s := (*S)(allocCounted(n))
	p := &s.f
	*p = 1
	// Dec стоїть на базі, як його ставить прохід після останнього вживання s.
	decAccounted(unsafe.Pointer(s), n)
	println(*p)
}
