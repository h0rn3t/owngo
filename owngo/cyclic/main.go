//go:build owngo

// Програма прийняття P6.1: DecCyclic виконує trial deletion до повернення.
// Кільце двох вузлів звільняється після зняття обох коренів, додатковий Inc
// його тримає, вузол без ребра звільняється на нулі, zerobase лишається.
package main

import (
	"fmt"
	"os"
	"unsafe"
)

// Розкладка збігається з runtime.CyclicDesc і runtime.CyclicEdge.
type cyclicDesc struct {
	size  uintptr
	edges []cyclicEdge
}

type cyclicEdge struct {
	off  uintptr
	elem *cyclicDesc
}

type node struct {
	next unsafe.Pointer
	val  int
}

//go:linkname allocU github.com/xgo-dev/llgo/runtime/internal/runtime.AllocU
func allocU(size uintptr) unsafe.Pointer

//go:linkname allocCounted github.com/xgo-dev/llgo/runtime/internal/runtime.AllocCounted
func allocCounted(size uintptr) unsafe.Pointer

//go:linkname inc github.com/xgo-dev/llgo/runtime/internal/runtime.Inc
func inc(ptr unsafe.Pointer)

//go:linkname decCyclic github.com/xgo-dev/llgo/runtime/internal/runtime.DecCyclic
func decCyclic(ptr unsafe.Pointer, desc unsafe.Pointer)

//go:linkname noteAlloc github.com/xgo-dev/llgo/runtime/internal/runtime.NoteAlloc
func noteAlloc(size uintptr, cat int32)

//go:linkname freed github.com/xgo-dev/llgo/runtime/internal/runtime.Freed
func freed() uint64

//go:linkname live github.com/xgo-dev/llgo/runtime/internal/runtime.Live
func live(cat int32) uint64

const catCyclic = 3

var nodeDesc = cyclicDesc{size: unsafe.Sizeof(node{})}

func init() {
	nodeDesc.edges = []cyclicEdge{{off: unsafe.Offsetof(node{}.next), elem: &nodeDesc}}
}

func newNode() unsafe.Pointer {
	p := allocCounted(nodeDesc.size)
	*(*node)(p) = node{} // AllocCounted не обнуляє, як і emitAllocCounted до запису нуля
	noteAlloc(nodeDesc.size, catCyclic)
	return p
}

// link записує b у поле next вузла a і рахує це ребро.
func link(a, b unsafe.Pointer) {
	(*node)(a).next = b
	inc(b)
}

func count(p unsafe.Pointer) uintptr {
	return *(*uintptr)(unsafe.Add(p, -int(unsafe.Sizeof(uintptr(0)))))
}

func main() {
	desc := unsafe.Pointer(&nodeDesc)

	// Кільце двох вузлів: обидва звільняються, коли знято обидва корені.
	a, b := newNode(), newNode()
	link(a, b)
	link(b, a)
	beforeFreed, beforeLive := freed(), live(catCyclic)
	decCyclic(a, desc)
	if count(a) != 1 || count(b) != 2 || freed() != beforeFreed {
		fail("ring after first root", count(a), count(b))
	}
	decCyclic(b, desc)
	if got, want := freed()-beforeFreed, uint64(2*nodeDesc.size); got != want {
		fail("ring freed", got, want)
	}
	if got, want := beforeLive-live(catCyclic), uint64(2*nodeDesc.size); got != want {
		fail("ring live", got, want)
	}

	// Те саме кільце з додатковим Inc лишається виділеним, слова відновлені.
	a, b = newNode(), newNode()
	link(a, b)
	link(b, a)
	inc(a)
	beforeFreed = freed()
	decCyclic(a, desc)
	decCyclic(b, desc)
	if freed() != beforeFreed {
		fail("held ring freed", freed()-beforeFreed)
	}
	if count(a) != 2 || count(b) != 1 || (*node)(a).next != b || (*node)(b).next != a {
		fail("held ring words", count(a), count(b))
	}

	// Вузол без ребра звільняється, коли слово доходить до нуля.
	c := newNode()
	inc(c)
	beforeFreed = freed()
	decCyclic(c, desc)
	if count(c) != 1 || freed() != beforeFreed {
		fail("single keeps", count(c))
	}
	decCyclic(c, desc)
	if got, want := freed()-beforeFreed, uint64(nodeDesc.size); got != want {
		fail("single freed", got, want)
	}

	// DecCyclic від zerobase підсумок не змінює.
	zero := allocU(0)
	beforeFreed = freed()
	decCyclic(zero, desc)
	if allocU(0) != zero || freed() != beforeFreed {
		fail("zerobase")
	}
	println("cyclic-ok")
}

func fail(reason string, args ...any) {
	fmt.Fprintln(os.Stderr, append([]any{reason}, args...)...)
	os.Exit(1)
}
