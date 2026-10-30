//go:build owngo

// Програма прийняття P3.5: одне базове купове місце rc-local звільняє свої
// 8 байтів, коли лічильник доходить до нуля.
package main

import (
	"os"
	_ "unsafe"
)

//go:linkname ownGoFreed github.com/xgo-dev/llgo/runtime/internal/runtime.Freed
func ownGoFreed() uint64

//go:linkname ownGoLive github.com/xgo-dev/llgo/runtime/internal/runtime.Live
func ownGoLive(cat int32) uint64

func countedSite() [8]byte {
	buf := [8]byte{1}
	read := func() [8]byte { return buf }
	return read()
}

func main() {
	beforeFreed := ownGoFreed()
	beforeLive := ownGoLive(1) // CatRCLocal
	if countedSite()[0] != 1 {
		os.Exit(2)
	}
	if got, want := ownGoFreed()-beforeFreed, uint64(8); got != want {
		println("freed delta", got, "want", want)
		os.Exit(3)
	}
	if got, want := ownGoLive(1), beforeLive; got != want {
		println("live rc-local", got, "want", want)
		os.Exit(4)
	}
	os.Exit(0)
}
