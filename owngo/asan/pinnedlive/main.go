//go:build owngo

// Програма прийняття P10.4: кожен випадок P10.3 читає і пише закріплений
// об'єкт через збережений unsafe.Pointer чи uintptr після того, як функція
// місця повернулась, а батько (коробка, кільце) звільнився. ASan звіту не дає.
package main

import (
	"os"
	"unsafe"
)

type T struct{ a, b int }

type Box struct{ p *T }

type Node struct {
	next *Node
	data *T
}

var (
	addr uintptr
	keep [5]unsafe.Pointer
)

func local() {
	t := &T{a: 1}
	keep[0] = unsafe.Pointer(t)
}

func New(a int) *T { return &T{a: a} }

func callerPins() {
	p := New(2)
	addr = uintptr(unsafe.Pointer(p))
}

func stash(p *T) { keep[2] = unsafe.Pointer(p) }

func calleePins() int {
	p := &T{a: 3}
	stash(p)
	return p.a
}

func boxPins() int {
	t := &T{a: 4}
	keep[3] = unsafe.Pointer(t)
	b := &Box{p: t}
	return b.p.a
}

func ringPins() int {
	t := &T{a: 5}
	keep[4] = unsafe.Pointer(t)
	a := &Node{}
	b := &Node{}
	a.next = b
	b.next = a
	a.data = t
	return a.data.a
}

func main() {
	local()
	callerPins()
	sum := calleePins() + boxPins() + ringPins()
	objs := []*T{(*T)(keep[0]), (*T)(unsafe.Pointer(addr)), (*T)(keep[2]), (*T)(keep[3]), (*T)(keep[4])}
	for i, t := range objs {
		if t.a != i+1 {
			os.Exit(2)
		}
		t.b = t.a * 10
		sum += t.b
	}
	if sum != 3+4+5+150 {
		os.Exit(3)
	}
	println("asan-pinnedlive-ok")
}
