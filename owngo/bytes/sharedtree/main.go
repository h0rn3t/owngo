//go:build owngo

// Програма прийняття P9.4: горутина пише свіжий результат NewTree у
// захоплену змінну, як longLivedTree binarytrees. Каскад DecShared
// комірки звільняє і комірку, і дерево; WaitGroup теж звільнено.
package main

import (
	"os"
	"sync"
	"time"
	"unsafe"
)

//go:linkname ownGoFreed github.com/xgo-dev/llgo/runtime/internal/runtime.Freed
func ownGoFreed() uint64

//go:linkname ownGoLive github.com/xgo-dev/llgo/runtime/internal/runtime.Live
func ownGoLive(cat int32) uint64

const (
	catRCShared = 2
	catCyclic   = 3
	depth       = 4
)

type Tree struct{ Left, Right *Tree }

func NewTree(depth int) *Tree {
	if depth > 0 {
		return &Tree{Left: NewTree(depth - 1), Right: NewTree(depth - 1)}
	}
	return &Tree{}
}

func (t *Tree) Count() int {
	if t.Left == nil {
		return 1
	}
	return 1 + t.Right.Count() + t.Left.Count()
}

func longLived() int {
	var wg sync.WaitGroup
	var tree *Tree
	wg.Add(1)
	go func() {
		tree = NewTree(depth)
		wg.Done()
	}()
	wg.Wait()
	return tree.Count()
}

func main() {
	beforeFreed := ownGoFreed()
	beforeShared, beforeCyclic := ownGoLive(catRCShared), ownGoLive(catCyclic)
	nodes := 1<<(depth+1) - 1
	if longLived() != nodes {
		os.Exit(2)
	}
	// Вузли, комірка змінної tree і WaitGroup.
	var cell *Tree
	want := uint64(uintptr(nodes)*unsafe.Sizeof(Tree{}) + unsafe.Sizeof(cell) + unsafe.Sizeof(sync.WaitGroup{}))
	for i := 0; i < 5000 && ownGoFreed()-beforeFreed < want; i++ {
		time.Sleep(time.Millisecond)
	}
	if got := ownGoFreed() - beforeFreed; got != want {
		println("freed delta", got, "want", want)
		os.Exit(3)
	}
	if got := ownGoLive(catRCShared); got != beforeShared {
		println("live rc-shared", got, "want", beforeShared)
		os.Exit(4)
	}
	if got := ownGoLive(catCyclic); got != beforeCyclic {
		println("live cyclic", got, "want", beforeCyclic)
		os.Exit(5)
	}
	os.Exit(0)
}
