//go:build owngo

// Програма прийняття P6.8: один DecCyclic у горутині звільняє ланцюг із
// N вузлів, не ростучи стеком з глибиною. N за замовчуванням 10 000 000,
// аргумент задає інше (для ASan — 100 000).
package main

import (
	"fmt"
	"os"
	"strconv"
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
	next *node
	val  int
}

//go:linkname allocCounted github.com/xgo-dev/llgo/runtime/internal/runtime.AllocCounted
func allocCounted(size uintptr) unsafe.Pointer

//go:linkname decCyclic github.com/xgo-dev/llgo/runtime/internal/runtime.DecCyclic
func decCyclic(ptr unsafe.Pointer, desc unsafe.Pointer)

//go:linkname freed github.com/xgo-dev/llgo/runtime/internal/runtime.Freed
func freed() uint64

var nodeDesc = cyclicDesc{size: unsafe.Sizeof(node{})}

func init() {
	nodeDesc.edges = []cyclicEdge{{off: unsafe.Offsetof(node{}.next), elem: &nodeDesc}}
}

// chain будує ланцюг так само, як свіжий результат у проході: AllocCounted
// з обнуленням, а одиниця нового вузла переміщується в поле next без Inc.
func chain(n int) *node {
	head := (*node)(allocCounted(unsafe.Sizeof(node{})))
	*head = node{}
	tail := head
	for i := 1; i < n; i++ {
		next := (*node)(allocCounted(unsafe.Sizeof(node{})))
		*next = node{val: i}
		tail.next = next
		tail = next
	}
	return head
}

func main() {
	n := 10_000_000
	if len(os.Args) > 1 {
		var err error
		if n, err = strconv.Atoi(os.Args[1]); err != nil || n < 1 {
			fmt.Fprintln(os.Stderr, "bad n", os.Args[1])
			os.Exit(2)
		}
	}
	head := chain(n)
	done := make(chan uint64)
	go func() {
		before := freed()
		decCyclic(unsafe.Pointer(head), unsafe.Pointer(&nodeDesc))
		done <- freed() - before
	}()
	if got, want := <-done, uint64(n)*uint64(nodeDesc.size); got != want {
		fmt.Fprintln(os.Stderr, "freed", got, "want", want)
		os.Exit(1)
	}
	println("cyclicdeep-ok", n)
}
