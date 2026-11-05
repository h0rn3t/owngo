//go:build owngo

// Програма прийняття P-NEW-05.4: будівник tree.New в іншому пакеті, корінь
// тут. Поки дерево живе, його байти — у Live(CatRegion); після останнього
// читання регіон звільнено цілим, і Freed зріс рівно на байти вузлів. Корінь,
// після створення якого вилітає паніка, лишає регіон живим. Вивід — як у go:
// кількості вузлів.
package main

import (
	"fmt"
	"os"
	"unsafe"

	"github.com/xgo-dev/llgo/owngo/regiontree/tree"
)

const catRegion = 6 // runtime.CatRegion

//go:linkname freed github.com/xgo-dev/llgo/runtime/internal/runtime.Freed
func freed() uint64

//go:linkname live github.com/xgo-dev/llgo/runtime/internal/runtime.Live
func live(cat int32) uint64

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

// measure будує дерево, читає Live(CatRegion) посеред його життя і Freed
// до й після; між ними нічого, крім дерева, не звільняється.
func measure(depth int) (n int, mid, delta uint64) {
	before := freed()
	t := tree.New(depth)
	mid = live(catRegion)
	n = t.Count()
	return n, mid, freed() - before
}

//go:noinline
func boom(depth int) {
	if depth >= 0 {
		panic("boom")
	}
}

// leaked: паніка між побудовою і останнім читанням, тож FreeRegion не
// виконується, а байти лишаються в Live(CatRegion).
func leaked(depth int) (n int) {
	defer func() {
		if recover() != nil {
			n = -1
		}
	}()
	return panicky(depth)
}

func panicky(depth int) int {
	t := tree.New(depth)
	boom(depth)
	return t.Count()
}

func main() {
	node := uint64(unsafe.Sizeof(tree.Tree{}))
	for _, depth := range []int{0, 4, 12} {
		n, mid, delta := measure(depth)
		want := 1<<(depth+1) - 1
		if n != want {
			fail("depth %d: Count = %d, want %d", depth, n, want)
		}
		// Live(CatRegion) росте, коли регіон бере наступний шматок 8 КіБ,
		// тож видно лише дерево, більше за шматок.
		if depth == 12 && mid == 0 {
			fail("depth %d: Live(region) = 0 while the tree is live, want the tree in a region", depth)
		}
		if delta != uint64(n)*node {
			fail("depth %d: Freed delta = %d, want %d", depth, delta, uint64(n)*node)
		}
		if got := live(catRegion); got != 0 {
			fail("depth %d: Live(region) = %d after the last read, want 0", depth, got)
		}
		fmt.Println("depth", depth, "nodes", n)
	}
	if n := leaked(12); n != -1 {
		fail("leaked(12) = %d, want -1 after recover", n)
	}
	if got := live(catRegion); got == 0 {
		fmt.Fprintln(os.Stderr, "Live(region) = 0 after a panic, want the region bytes live")
		os.Exit(1)
	}
	fmt.Println("recovered")
}
