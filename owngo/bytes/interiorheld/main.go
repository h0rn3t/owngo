//go:build owngo

// Програма прийняття P8.6: невідомий виклик утримує &s.f, тож 16 байтів S
// лишаються серед живих rc-local.
package main

import (
	"os"
	_ "unsafe"
)

//go:linkname ownGoFreed github.com/xgo-dev/llgo/runtime/internal/runtime.Freed
func ownGoFreed() uint64

//go:linkname ownGoLive github.com/xgo-dev/llgo/runtime/internal/runtime.Live
func ownGoLive(cat int32) uint64

// S — 16 байтів.
type S struct{ a, f int }

func touch(p *int) { *p++ }

func held(f func(*int), n int) int {
	s := S{a: n}
	get := func() int { return s.a }
	f(&s.f)
	return get() + s.f
}

func main() {
	beforeFreed := ownGoFreed()
	beforeLive := ownGoLive(1) // CatRCLocal
	if held(touch, 4) != 5 {
		os.Exit(2)
	}
	if got, want := ownGoFreed(), beforeFreed; got != want {
		println("freed", got, "want", want)
		os.Exit(3)
	}
	if got, want := ownGoLive(1)-beforeLive, uint64(16); got != want {
		println("live rc-local delta", got, "want", want)
		os.Exit(4)
	}
	os.Exit(0)
}
