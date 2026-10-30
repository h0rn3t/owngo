//go:build owngo

// Програма прийняття P3.4: читання після DecAccounted до нуля має завершитись звітом ASan.
package main

import "unsafe"

//go:linkname allocCounted github.com/xgo-dev/llgo/runtime/internal/runtime.AllocCounted
func allocCounted(size uintptr) unsafe.Pointer

//go:linkname decAccounted github.com/xgo-dev/llgo/runtime/internal/runtime.DecAccounted
func decAccounted(ptr unsafe.Pointer, size uintptr)

func main() {
	n := unsafe.Sizeof(int(0))
	p := allocCounted(n)
	ip := (*int)(p)
	*ip = 1
	decAccounted(p, n)
	println(*ip)
}
