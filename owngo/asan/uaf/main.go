//go:build owngo

// Програма прийняття P1.4: читання після runtime.Free має завершитись звітом ASan.
package main

import "unsafe"

//go:linkname free github.com/xgo-dev/llgo/runtime/internal/runtime.Free
func free(ptr unsafe.Pointer)

func main() {
	p := new(int)
	*p = 1
	free(unsafe.Pointer(p))
	println(*p)
}
