//go:build owngo

// Програма прийняття P9.5: читання спільного об'єкта після останнього
// DecShared має завершитись звітом ASan. Горутина віддає своє посилання
// першою, main — останнім.
package main

import "unsafe"

// Розкладка збігається з runtime.SharedDesc.
type sharedDesc struct {
	size  uintptr
	cat   int32
	edges []struct{ off, shared, cyclic, size uintptr }
}

//go:linkname allocCounted github.com/xgo-dev/llgo/runtime/internal/runtime.AllocCounted
func allocCounted(size uintptr) unsafe.Pointer

//go:linkname incShared github.com/xgo-dev/llgo/runtime/internal/runtime.IncShared
func incShared(ptr unsafe.Pointer)

//go:linkname decShared github.com/xgo-dev/llgo/runtime/internal/runtime.DecShared
func decShared(ptr unsafe.Pointer, desc unsafe.Pointer)

var intDesc = sharedDesc{size: unsafe.Sizeof(int(0)), cat: 2}

func main() {
	p := allocCounted(unsafe.Sizeof(int(0)))
	ip := (*int)(p)
	*ip = 1
	incShared(p)
	done := make(chan struct{})
	go func() {
		decShared(p, unsafe.Pointer(&intDesc))
		close(done)
	}()
	<-done
	decShared(p, unsafe.Pointer(&intDesc))
	println(*ip)
}
