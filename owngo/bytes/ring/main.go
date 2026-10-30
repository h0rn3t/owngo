//go:build owngo

// Програма прийняття P6.3: кільце двох базових вузлів звільняє їхні байти
// користувача, коли знято обидва корені.
package main

import (
	"os"
	"unsafe"
)

//go:linkname ownGoFreed github.com/xgo-dev/llgo/runtime/internal/runtime.Freed
func ownGoFreed() uint64

//go:linkname ownGoLive github.com/xgo-dev/llgo/runtime/internal/runtime.Live
func ownGoLive(cat int32) uint64

type node struct {
	next *node
	val  int
}

func ringSite() int {
	a := &node{val: 1}
	b := &node{val: 2}
	a.next = b
	b.next = a
	return a.val + b.next.val
}

func main() {
	beforeFreed := ownGoFreed()
	beforeLive := ownGoLive(3) // CatCyclic
	if ringSite() != 2 {
		os.Exit(2)
	}
	want := uint64(2 * unsafe.Sizeof(node{}))
	if got := ownGoFreed() - beforeFreed; got != want {
		println("freed delta", got, "want", want)
		os.Exit(3)
	}
	if got := ownGoLive(3); got != beforeLive {
		println("live cyclic", got, "want", beforeLive)
		os.Exit(4)
	}
	os.Exit(0)
}
