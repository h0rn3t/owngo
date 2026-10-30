//go:build owngo

// Програма прийняття P6.4: читання вузла після trial deletion, яка звільнила
// кільце, має завершитись звітом ASan.
package main

import "unsafe"

//go:linkname allocCounted github.com/xgo-dev/llgo/runtime/internal/runtime.AllocCounted
func allocCounted(size uintptr) unsafe.Pointer

//go:linkname inc github.com/xgo-dev/llgo/runtime/internal/runtime.Inc
func inc(ptr unsafe.Pointer)

//go:linkname decCyclic github.com/xgo-dev/llgo/runtime/internal/runtime.DecCyclic
func decCyclic(ptr unsafe.Pointer, desc unsafe.Pointer)

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
	next *node
	val  int
}

var nodeDesc = cyclicDesc{size: unsafe.Sizeof(node{})}

func init() {
	nodeDesc.edges = []cyclicEdge{{off: unsafe.Offsetof(node{}.next), elem: &nodeDesc}}
}

// ring будує кільце двох вузлів так само, як прохід: AllocCounted
// з обнуленням і Inc на кожен запис у next.
func ring() (a, b *node) {
	a = (*node)(allocCounted(unsafe.Sizeof(node{})))
	b = (*node)(allocCounted(unsafe.Sizeof(node{})))
	*a, *b = node{val: 1}, node{val: 2}
	a.next = b
	inc(unsafe.Pointer(b))
	b.next = a
	inc(unsafe.Pointer(a))
	return a, b
}

func main() {
	a, b := ring()
	desc := unsafe.Pointer(&nodeDesc)
	decCyclic(unsafe.Pointer(a), desc)
	decCyclic(unsafe.Pointer(b), desc)
	println(a.val)
}
