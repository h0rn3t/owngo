// fannkuch-redux за зразком програми Rust #6 з Benchmarks Game (Henry
// Jayakusuma, за C++ #6 Andrei Simion), Revised BSD, див. LICENSE:
// https://benchmarksgame-team.pages.debian.net/benchmarksgame/program/fannkuchredux-rust-6.html
//
// Rust #6 тримає перестановку в u128 по байту на елемент і обертає та
// перевертає її через pshufb з таблиць масок. У Go інтринсиків SSE немає,
// тож перестановка — uint64 по півбайту на елемент (n ≤ 16), а обидві
// операції — зсуви й маски без циклу. Робота поділена так само: n блоків
// за обертанням усіх n елементів, кожен — на n-1 блоків за обертанням
// перших n-1, і всі n·(n-1) блоків бере пул робітників, як rayon.
package main

import (
	"fmt"
	"math/bits"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
)

// identity — перестановка 0, 1, …, 15: елемент i у півбайті i.
const identity uint64 = 0xFEDCBA9876543210

// prefix — маска перших k півбайтів. Зсув на 64 у Go дає 0, тож k = 16 —
// уся перестановка.
func prefix(k int) uint64 {
	return 1<<(4*k) - 1
}

// reverse перевертає перші k+1 елементів: розвертає всі 16 півбайтів і
// зсуває розвернуту частину на місце префікса.
func reverse(perm uint64, k int) uint64 {
	r := bits.ReverseBytes64(perm)
	r = r>>4&0x0F0F0F0F0F0F0F0F | r&0x0F0F0F0F0F0F0F0F<<4
	m := prefix(k + 1)
	return r>>(4*(15-k))&m | perm&^m
}

// rotate зсуває перші k+1 елементів на один ліворуч, перший стає k-м.
func rotate(perm uint64, k int) uint64 {
	m := prefix(k + 1)
	return (perm&m)>>4 | (perm&0xF)<<(4*k) | perm&^m
}

// advance — наступна перестановка за лічильниками count, як advance_array.
func advance(perm uint64, count *[16]uint8) uint64 {
	for layer := 1; layer < 16; layer++ {
		perm = rotate(perm, layer)
		count[layer]++
		if int(count[layer]) <= layer {
			break
		}
		count[layer] = 0
	}
	return perm
}

// block обходить перестановки, доки лічильник n-2 не зміниться, і повертає
// їхню контрольну суму і найбільшу кількість перевертань.
func block(perm uint64, count [16]uint8, n int) (checksum, maxFlips int) {
	start := count[n-2]
	for count[n-2] == start {
		if first := int(perm & 0xF); first > 0 {
			flips := 0
			for tmp := perm; first > 0; flips++ {
				next := int(tmp >> (4 * first) & 0xF)
				tmp = reverse(tmp, first)
				first = next
			}
			// Парна перестановка додає flips, непарна віднімає.
			sign := int(count[1])
			checksum += flips ^ -sign + sign
			maxFlips = max(maxFlips, flips)
		}
		perm = advance(perm, &count)
	}
	return checksum, maxFlips
}

func run(n int) []byte {
	if n > 16 {
		panic("fannkuch-redux: n > 16")
	}
	checksum, maxFlips := 0, 0
	switch n {
	case 0, 1:
	case 2:
		checksum, maxFlips = -1, 1
	default:
		checksum, maxFlips = fannkuch(n)
	}
	return fmt.Appendf(nil, "%d\nPfannkuchen(%d) = %d\n", checksum, n, maxFlips)
}

func fannkuch(n int) (checksum, maxFlips int) {
	type task struct {
		perm  uint64
		count [16]uint8
	}
	tasks := make([]task, 0, n*(n-1))
	outer := identity
	for i := range n {
		inner := outer
		for j := range n - 1 {
			var count [16]uint8
			count[n-1], count[n-2] = uint8(i), uint8(j)
			tasks = append(tasks, task{inner, count})
			inner = rotate(inner, n-2)
		}
		outer = rotate(outer, n-1)
	}

	sums := make([]int, len(tasks))
	flips := make([]int, len(tasks))
	var next atomic.Int64
	var wg sync.WaitGroup
	for range min(runtime.NumCPU(), len(tasks)) {
		wg.Go(func() {
			for {
				k := next.Add(1) - 1
				if k >= int64(len(tasks)) {
					break
				}
				sums[k], flips[k] = block(tasks[k].perm, tasks[k].count, n)
			}
		})
	}
	wg.Wait()

	for k := range tasks {
		checksum += sums[k]
		maxFlips = max(maxFlips, flips[k])
	}
	return checksum, maxFlips
}

func main() {
	n := 7
	if len(os.Args) > 1 {
		if v, err := strconv.Atoi(os.Args[1]); err == nil {
			n = v
		}
	}
	if _, err := os.Stdout.Write(run(n)); err != nil {
		os.Exit(1)
	}
}
