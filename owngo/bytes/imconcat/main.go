//go:build owngo

// Програма прийняття P5.2: доведена конкатенація static звільняє свої 8 байтів.
package main

import (
	"os"
	_ "unsafe"
)

//go:linkname ownGoFreed github.com/xgo-dev/llgo/runtime/internal/runtime.Freed
func ownGoFreed() uint64

//go:linkname ownGoLive github.com/xgo-dev/llgo/runtime/internal/runtime.Live
func ownGoLive(cat int32) uint64

func concatSite(a, b string) byte {
	s := a + b
	return s[0]
}

func main() {
	beforeFreed := ownGoFreed()
	beforeLive := ownGoLive(0) // CatStatic
	if concatSite("abcd", "efgh") != 'a' {
		os.Exit(2)
	}
	if got, want := ownGoFreed()-beforeFreed, uint64(8); got != want {
		println("freed delta", got, "want", want)
		os.Exit(3)
	}
	if got, want := ownGoLive(0), beforeLive; got != want {
		println("live static", got, "want", want)
		os.Exit(4)
	}
	os.Exit(0)
}
