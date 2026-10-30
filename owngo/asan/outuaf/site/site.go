//go:build owngo

// Package site виділяє int поза пакетом main і читає його після FreeAccounted.
package site

import "unsafe"

//go:linkname freeAccounted github.com/xgo-dev/llgo/runtime/internal/runtime.FreeAccounted
func freeAccounted(ptr unsafe.Pointer, size uintptr, cat int32)

// ReadAfterFree виділяє int, звільняє його через FreeAccounted і читає звільнену пам'ять.
func ReadAfterFree() {
	p := new(int)
	*p = 1
	freeAccounted(unsafe.Pointer(p), unsafe.Sizeof(*p), 0) // CatStatic
	println(*p)
}
