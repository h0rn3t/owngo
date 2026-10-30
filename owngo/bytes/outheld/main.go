//go:build owngo

// Програма прийняття P4.2: невідомий виклик утримує місце поза main.
// v1 позначає його rc-local: shared і не ставить Dec, тож 8 байтів лишаються живими.
package main

import (
	"os"
	_ "unsafe"

	"github.com/xgo-dev/llgo/owngo/bytes/outheld/site"
)

//go:linkname ownGoFreed github.com/xgo-dev/llgo/runtime/internal/runtime.Freed
func ownGoFreed() uint64

//go:linkname ownGoLive github.com/xgo-dev/llgo/runtime/internal/runtime.Live
func ownGoLive(cat int32) uint64

func touch(p *[8]byte) { _ = p[0] }

func main() {
	beforeFreed := ownGoFreed()
	beforeStatic := ownGoLive(0) // CatStatic
	beforeLocal := ownGoLive(1)  // CatRCLocal
	if site.Held(touch) != 7 {
		os.Exit(2)
	}
	if got, want := ownGoFreed(), beforeFreed; got != want {
		println("freed", got, "want", want)
		os.Exit(3)
	}
	if got, want := ownGoLive(0), beforeStatic; got != want {
		println("live static", got, "want", want)
		os.Exit(4)
	}
	if got, want := ownGoLive(1)-beforeLocal, uint64(8); got != want {
		println("live rc-local delta", got, "want", want)
		os.Exit(5)
	}
	os.Exit(0)
}
