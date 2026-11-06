//go:build owngo

// Програма прийняття P-NEW-03.3: місце static, мертве на вході блока злиття,
// звільняється на цьому вході (topBreak) чи на розщепленому ребрі (breakSet,
// ifUse).
// Під -asan — код 0 без тексту ASan, байти static після викликів не живі.
package main

import (
	"os"

	_ "unsafe"
)

//go:linkname live github.com/xgo-dev/llgo/runtime/internal/runtime.Live
func live(cat int32) uint64

const catStatic = 0 // runtime.CatStatic

func fill(t *[8]int, i int) { t[i&7] = i }

func topBreak(n int) int {
	var flag [8]int
	acc := 0
	for i := 0; ; i++ {
		if i >= n {
			break
		}
		fill(&flag, i)
		acc += flag[i&7]
		if acc > 5 {
			break
		}
	}
	return acc
}

func breakSet(n int) int {
	var flag [8]int
	acc := 0
	for i := 0; ; i++ {
		if i >= n {
			break
		}
		fill(&flag, i)
		if flag[i&7] > 5 {
			acc = -1
			break
		}
		acc += flag[i&7]
	}
	return acc
}

func ifUse(n int) int {
	var t [8]int
	fill(&t, n)
	x := 0
	if n > 0 {
		x = t[1]
	}
	return x
}

func main() {
	before := live(catStatic)
	sum := 0
	for n := range 10 {
		sum += topBreak(n) + breakSet(n) + ifUse(n-5)
	}
	if sum != 73 {
		println("sum", sum)
		os.Exit(2)
	}
	if after := live(catStatic); after != before {
		println("live static", before, "->", after)
		os.Exit(3)
	}
	println("asan-mergefree-ok")
}
