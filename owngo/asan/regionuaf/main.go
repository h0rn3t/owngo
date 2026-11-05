//go:build owngo

// Програма прийняття P-NEW-05.3: читання після FreeRegion під -asan має
// завершитись звітом heap-use-after-free.
package main

import "unsafe"

//go:linkname regionCreate github.com/xgo-dev/llgo/runtime/internal/runtime.RegionCreate
func regionCreate() unsafe.Pointer

//go:linkname regionAlloc github.com/xgo-dev/llgo/runtime/internal/runtime.RegionAlloc
func regionAlloc(r unsafe.Pointer, size uintptr) unsafe.Pointer

//go:linkname freeRegion github.com/xgo-dev/llgo/runtime/internal/runtime.FreeRegion
func freeRegion(r unsafe.Pointer)

func main() {
	r := regionCreate()
	p := (*int)(regionAlloc(r, unsafe.Sizeof(0)))
	*p = 1
	freeRegion(r)
	println(*p)
}
