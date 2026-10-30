//go:build owngo

// Програма прийняття P1.5: одне купове місце static звільняє свої 8 байтів.
package main

import (
	"os"
	_ "unsafe"
)

//go:linkname ownGoFreed github.com/xgo-dev/llgo/runtime/internal/runtime.Freed
func ownGoFreed() uint64

//go:linkname ownGoLive github.com/xgo-dev/llgo/runtime/internal/runtime.Live
func ownGoLive(cat int32) uint64

func staticSite() byte {
	p := new([8]byte)
	p[0] = 1
	return p[0]
}

func main() {
	beforeFreed := ownGoFreed()
	beforeLive := ownGoLive(0) // CatStatic
	if staticSite() != 1 {
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
