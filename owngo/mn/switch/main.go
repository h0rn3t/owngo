//go:build owngo && owngomn

// Програма прийняття P-NEW-09.5: перемикання контексту і стеки G з резерву.
//
//   - Дві G перемикаються 10⁶ разів; кожна веде свою послідовність у
//     локальних змінних, що живуть через перемикання, тож зіпсований регістр,
//     який зберігає викликаний, або sp дає іншу суму.
//   - 10 000 G з резервом 1 МіБ, кожна запущена й припаркована: приріст RSS
//     не більший за 10 000 × 2 сторінки плюс 16 МіБ.
//
// Код 0 лише якщо обидві перевірки пройшли; час одного перемикання —
// рядок mn-switch-ns на stdout.
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	_ "unsafe"
)

//go:linkname mnSwitch github.com/xgo-dev/llgo/runtime/internal/runtime.MNSwitchForTesting
func mnSwitch(rounds int) bool

//go:linkname mnPark github.com/xgo-dev/llgo/runtime/internal/runtime.MNParkForTesting
func mnPark(n int, stack uintptr, whileParked func()) bool

// resident повертає RSS процесу в байтах (/proc/self/statm).
func resident() int64 {
	b, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		fmt.Fprintln(os.Stderr, "statm:", err)
		os.Exit(9)
	}
	f := strings.Fields(string(b))
	pages, _ := strconv.ParseInt(f[1], 10, 64)
	return pages * int64(os.Getpagesize())
}

func main() {
	const rounds = 1_000_000
	start := time.Now()
	if !mnSwitch(rounds) {
		println("switch: sums differ")
		os.Exit(2)
	}
	// Кожен раунд — два перемикання: туди й назад.
	fmt.Printf("mn-switch-ns %.1f\n", float64(time.Since(start).Nanoseconds())/(2*rounds))

	const n, stack = 10_000, 1 << 20
	before := resident()
	var grown int64
	if !mnPark(n, stack, func() { grown = resident() - before }) {
		println("park: failed")
		os.Exit(3)
	}
	limit := int64(n*2*os.Getpagesize() + 16<<20)
	fmt.Printf("mn-park-rss %d limit %d\n", grown, limit)
	if grown > limit {
		os.Exit(4)
	}
}
