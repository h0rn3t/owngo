//go:build owngo

// Програма прийняття S2: облік рантайму з восьми горутин. Код 0 лише якщо
// зміна Freed і Live кожної категорії точна. Аргумент n — ітерації на
// горутину, за замовчуванням 1 000 000.
//
// Горутина g n разів робить NoteAlloc пари g і FreeAccounted пари g+1, тож
// кожну пару облічують і звільняють різні горутини: шард потоку може
// піти в мінус, сума — ні.
package main

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"unsafe"
)

const (
	workers  = 8
	catCount = 6 // runtime.CatStatic … CatImplicit
)

//go:linkname allocCounted github.com/xgo-dev/llgo/runtime/internal/runtime.AllocCounted
func allocCounted(size uintptr) unsafe.Pointer

//go:linkname noteAlloc github.com/xgo-dev/llgo/runtime/internal/runtime.NoteAlloc
func noteAlloc(size uintptr, cat int32)

//go:linkname freeAccounted github.com/xgo-dev/llgo/runtime/internal/runtime.FreeAccounted
func freeAccounted(ptr unsafe.Pointer, size uintptr, cat int32)

//go:linkname freed github.com/xgo-dev/llgo/runtime/internal/runtime.Freed
func freed() uint64

//go:linkname live github.com/xgo-dev/llgo/runtime/internal/runtime.Live
func live(cat int32) uint64

type snapshot struct {
	freed uint64
	live  [catCount]uint64
}

func take() snapshot {
	s := snapshot{freed: freed()}
	for cat := range s.live {
		s.live[cat] = live(int32(cat))
	}
	return s
}

// Бар'єри — атомарні змінні: між знімками фікстура не виділяє і не
// звільняє, тож у різницю потрапляє лише облік з циклів горутин.
var ready, finished, start, release atomic.Int32

func wait(flag *atomic.Int32, want int32) {
	for flag.Load() != want {
		runtime.Gosched()
	}
}

func main() {
	n := 1_000_000
	if len(os.Args) > 1 {
		var err error
		if n, err = strconv.Atoi(os.Args[1]); err != nil || n < 1 {
			fmt.Fprintln(os.Stderr, "bad n", os.Args[1])
			os.Exit(2)
		}
	}
	// Пара g: розмір g+1 байтів, категорія g mod 6 — усі шість категорій.
	var sizes [workers]uintptr
	var cats [workers]int32
	var wantFreed uint64
	for g := range workers {
		sizes[g] = uintptr(g + 1)
		cats[g] = int32(g % catCount)
		wantFreed += uint64(n) * uint64(g+1)
	}
	zero := allocCounted(0) // FreeAccounted на zerobase лише рахує

	var wg sync.WaitGroup
	for g := range workers {
		own, next := g, (g+1)%workers
		wg.Go(func() {
			ready.Add(1)
			wait(&start, 1)
			for range n {
				noteAlloc(sizes[own], cats[own])
				freeAccounted(zero, sizes[next], cats[next])
			}
			finished.Add(1)
			wait(&release, 1)
		})
	}
	wait(&ready, workers)
	before := take()
	start.Store(1)
	wait(&finished, workers)
	after := take()
	release.Store(1)
	wg.Wait()

	if got := after.freed - before.freed; got != wantFreed {
		fmt.Fprintln(os.Stderr, "freed delta", got, "want", wantFreed)
		os.Exit(1)
	}
	for cat := range catCount {
		if after.live[cat] != before.live[cat] {
			fmt.Fprintln(os.Stderr, "live", cat, "before", before.live[cat], "after", after.live[cat])
			os.Exit(1)
		}
	}
	println("accountshard-ok", n)
}
