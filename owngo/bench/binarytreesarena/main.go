// binary-trees за зразком програми Rust з Benchmarks Game (bumpalo і rayon,
// автори перелічені на сторінці бенчмарку): дерево живе в арені, а не у
// вузлах окремих malloc, і вся робота — одна черга шматків для всіх ядер.
//
// Арена — масив вузлів, виділений один раз на робітника і перевикористаний
// для кожного дерева. Діти — індекси в масиві, тож на дерево немає жодної
// алокації чи звільнення. Вивід той самий, що в Go #2.
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
)

// node — вузол у арені. Корінь має індекс 0 і дитиною не буває, тож
// нульові індекси означають лист.
type node struct {
	left, right int32
}

// arena — масив вузлів одного робітника. Дерево лежить у ньому в порядку
// Rust #5: вузол, праве піддерево, ліве, тож позиції дітей обчислюються,
// і під час побудови немає спільного лічильника, у який пишуть на кожному
// вузлі: малі структури різних робітників ділили б кеш-лінію. Методи
// беруть вказівник, бо зріз за значенням llgo копіює через стек на кожен
// виклик.
type arena struct {
	nodes []node
}

// newArena вміщує повне дерево глибини depth: 2^(depth+1)-1 вузлів.
func newArena(depth int) *arena {
	return &arena{nodes: make([]node, 1<<(depth+1)-1)}
}

// tree будує повне дерево глибини depth з коренем у вузлі i. Праве
// піддерево займає 2^depth-1 вузлів після i, ліве йде одразу за ним.
// depth беззнаковий, щоб зсув не перевірявся на від'ємність.
func (a *arena) tree(i int32, depth uint) {
	if depth == 0 {
		a.nodes[i] = node{}
		return
	}
	right, left := i+1, i+1<<depth
	a.nodes[i] = node{left: left, right: right}
	a.tree(right, depth-1)
	a.tree(left, depth-1)
}

func (a *arena) check(i int32) int {
	n := a.nodes[i]
	if n.left == 0 {
		return 1
	}
	return 1 + a.check(n.right) + a.check(n.left)
}

func run(n int) []byte {
	const minDepth = 4
	maxDepth := max(n, minDepth+2)

	// Уся робота — одна черга: stretch-дерево і довгоживуче дерево у своїх
	// горутинах, а ітерації всіх глибин порізані на шматки, які робітники
	// беруть атомарним лічильником, як rayon краде задачі. Глибини не чекають
	// одна одну, і ядро без роботи не простоює до кінця глибини.
	// Не wg.Go: owngo звільняє змінні, захоплені замиканням, одразу після
	// виклику, бо не бачить, що WaitGroup.Go передає f у go.
	var wg sync.WaitGroup
	var stretchCheck int
	wg.Add(2)
	go func() {
		a := newArena(maxDepth + 1)
		a.tree(0, uint(maxDepth+1))
		stretchCheck = a.check(0)
		wg.Done()
	}()
	longLived := newArena(maxDepth)
	go func() {
		longLived.tree(0, uint(maxDepth))
		wg.Done()
	}()

	workers := runtime.NumCPU()
	type chunk struct{ depth, iterations int }
	var chunks []chunk
	// Спершу найглибші дерева: їхні шматки найдовші, тож хвіст черги дрібний.
	for depth := maxDepth - (maxDepth-minDepth)%2; depth >= minDepth; depth -= 2 {
		iterations := 1 << (maxDepth - depth + minDepth)
		parts := min(iterations, 4*workers)
		for p := range parts {
			chunks = append(chunks, chunk{depth, (p+1)*iterations/parts - p*iterations/parts})
		}
	}
	checks := make([]atomic.Int64, maxDepth+1)
	var next atomic.Int64
	for range workers {
		wg.Add(1)
		go func() { //nolint:modernize // owngo frees captures passed to wg.Go, see above
			a := newArena(maxDepth)
			for {
				k := next.Add(1) - 1
				if k >= int64(len(chunks)) {
					break
				}
				c := chunks[k]
				sum := 0
				for range c.iterations {
					a.tree(0, uint(c.depth))
					sum += a.check(0)
				}
				checks[c.depth].Add(int64(sum))
			}
			wg.Done()
		}()
	}
	wg.Wait()

	out := fmt.Appendf(nil, "stretch tree of depth %d\t check: %d\n", maxDepth+1, stretchCheck)
	for depth := minDepth; depth <= maxDepth; depth += 2 {
		out = fmt.Appendf(out, "%d\t trees of depth %d\t check: %d\n",
			1<<(maxDepth-depth+minDepth), depth, checks[depth].Load())
	}
	return fmt.Appendf(out, "long lived tree of depth %d\t check: %d\n",
		maxDepth, longLived.check(0))
}

func main() {
	n := 0
	flag.Parse()
	if flag.NArg() > 0 {
		n, _ = strconv.Atoi(flag.Arg(0))
	}
	if _, err := os.Stdout.Write(run(n)); err != nil {
		os.Exit(1)
	}
}
