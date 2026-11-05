//go:build owngo

// Програма прийняття P-NEW-05.3: дерево в регіоні читається до FreeRegion
// і більше не читається. Під -asan — код 0 без тексту ASan.
package main

import "unsafe"

//go:linkname regionCreate github.com/xgo-dev/llgo/runtime/internal/runtime.RegionCreate
func regionCreate() unsafe.Pointer

//go:linkname regionAlloc github.com/xgo-dev/llgo/runtime/internal/runtime.RegionAlloc
func regionAlloc(r unsafe.Pointer, size uintptr) unsafe.Pointer

//go:linkname freeRegion github.com/xgo-dev/llgo/runtime/internal/runtime.FreeRegion
func freeRegion(r unsafe.Pointer)

type node struct {
	left, right *node
}

func build(r unsafe.Pointer, depth int) *node {
	n := (*node)(regionAlloc(r, unsafe.Sizeof(node{})))
	if depth > 0 {
		n.left = build(r, depth-1)
		n.right = build(r, depth-1)
	}
	return n
}

func (n *node) count() int {
	if n.left == nil {
		return 1
	}
	return 1 + n.left.count() + n.right.count()
}

func main() {
	total := 0
	for depth := range 12 {
		r := regionCreate()
		total += build(r, depth).count()
		big := (*[1 << 16]byte)(regionAlloc(r, 1<<16))
		big[len(big)-1] = 1
		total += int(big[len(big)-1])
		freeRegion(r)
	}
	println("regionlive-ok", total)
}
