//go:build owngo

// Програма прийняття P-NEW-04.2: місце static сталого розміру, чий об'єкт не
// переживає ітерацію, лежить на стеку і обнуляється на кожному виконанні
// місця (staticNew, inLoop, appendArgs): FreeAccounted не рахує його байтів.
// Об'єкт, який phi, масив чи defer несе в наступну ітерацію, лишається
// окремим (carried, stored, deferred).
// Під -asan — код 0 без тексту ASan.
package main

import (
	"os"

	_ "unsafe"
)

//go:linkname freed github.com/xgo-dev/llgo/runtime/internal/runtime.Freed
func freed() uint64

func fill(t *[8]int, i int) { t[i&7] = i + 1 }

func staticNew(v int) int {
	p := new(int)
	*p += v
	return *p
}

func inLoop(n int) int {
	acc := 0
	for i := range n {
		var flag [8]int
		for _, x := range flag {
			if x != 0 {
				return -1 // об'єкт попередньої ітерації не обнулено
			}
		}
		fill(&flag, i)
		acc += flag[i&7]
	}
	return acc
}

func appendArgs(s []*int, n int) []*int {
	for range n {
		s = append(s, nil)
	}
	return s
}

func carried(n int) int {
	var prev *int
	acc := 0
	for i := range n {
		p := new(int)
		*p = i
		if prev != nil {
			acc += *prev
		}
		prev = p
	}
	return acc
}

func stored() int {
	var arr [4]*int
	for i := range 4 {
		p := new(int)
		*p = i
		arr[i] = p
	}
	return *arr[0]*1000 + *arr[1]*100 + *arr[2]*10 + *arr[3]
}

var order int

func record(p *int) { order = order*10 + *p }

func deferred(n int) {
	for i := range n {
		p := new(int)
		*p = i + 1
		defer record(p)
	}
}

func main() {
	before := freed()
	sum := 0
	for n := range 10 {
		sum += staticNew(n) + inLoop(n) + len(appendArgs(make([]*int, 0, 16), n))
	}
	if sum != 255 {
		println("sum", sum)
		os.Exit(2)
	}
	if after := freed(); after != before {
		println("freed", before, "->", after, "want no heap static")
		os.Exit(3)
	}
	if got := carried(5); got != 6 {
		println("carried", got, "want 6")
		os.Exit(4)
	}
	if got := stored(); got != 123 {
		println("stored", got, "want 123")
		os.Exit(5)
	}
	deferred(3)
	if order != 321 {
		println("deferred", order, "want 321")
		os.Exit(6)
	}
	println("asan-stackstatic-ok")
}
