//go:build owngo

// Програма прийняття P5.3: читання після FreeAccounted доведеної
// конкатенації має завершитись звітом ASan.
package main

import "unsafe"

//go:linkname freeAccounted github.com/xgo-dev/llgo/runtime/internal/runtime.FreeAccounted
func freeAccounted(ptr unsafe.Pointer, size uintptr, cat int32)

func readAfterFree(a, b string) {
	s := a + b
	freeAccounted(unsafe.Pointer(unsafe.StringData(s)), uintptr(len(s)), 0) // CatStatic
	println(s[0])
}

func main() {
	readAfterFree("abcd", "efgh")
}
