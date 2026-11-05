//go:build owngo

// Програма прийняття P-NEW-05.3 і P-NEW-05.4: дерево в регіоні читається до
// FreeRegion і більше не читається — і з рантаймом напряму, і з регіоном,
// який вивів компілятор (newTree — будівник, t — корінь). Під -asan — код 0
// без тексту ASan.
package main

import "unsafe"

//go:linkname regionCreate github.com/xgo-dev/llgo/runtime/internal/runtime.RegionCreate
func regionCreate() unsafe.Pointer

//go:linkname regionAlloc github.com/xgo-dev/llgo/runtime/internal/runtime.RegionAlloc
func regionAlloc(r unsafe.Pointer, size uintptr) unsafe.Pointer

//go:linkname freeRegion github.com/xgo-dev/llgo/runtime/internal/runtime.FreeRegion
func freeRegion(r unsafe.Pointer)

//go:linkname live github.com/xgo-dev/llgo/runtime/internal/runtime.Live
func live(cat int32) uint64

const catRegion = 6 // runtime.CatRegion

type tree struct {
	left, right *tree
}

func newTree(depth int) *tree {
	if depth > 0 {
		return &tree{left: newTree(depth - 1), right: newTree(depth - 1)}
	}
	return &tree{}
}

func (t *tree) count() int {
	if t.left == nil {
		return 1
	}
	return 1 + t.left.count() + t.right.count()
}

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
	for depth := range 12 {
		t := newTree(depth)
		if live(catRegion) == 0 {
			// Під -asan кожен вузол регіону — окремий блок, і Live росте з першого.
			panic("compiler region: Live(region) = 0 while the tree is live")
		}
		total += t.count()
	}
	if live(catRegion) != 0 {
		panic("compiler region: Live(region) != 0 after the last read")
	}
	println("regionlive-ok", total)
}
