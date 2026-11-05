//go:build owngo && owngostat

// Програма прийняття P-NEW-02.0: лічильники операцій рантайму під тегом
// owngostat. Вісім горутин n разів роблять відомий набір операцій; код 0
// лише якщо зміна кожного лічильника між знімками точна. Аргумент n —
// ітерації на горутину, за замовчуванням 100 000.
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

const workers = 8

// Номери збігаються з opAlloc … opNoteAlloc+5 у runtime/internal/runtime.
const (
	opAlloc = iota
	opAllocRoot
	opAllocCounted
	opInc
	opDecAccounted
	opDecCyclic
	opCyclicVisit
	opIncShared
	opDecShared
	opFreeAccounted
	opFreeRoot
	opNoteAlloc // + категорія 0…5
	opCount     = opNoteAlloc + 6
)

var opNames = [opCount]string{
	"alloc", "alloc_root", "alloc_counted", "inc", "dec_accounted",
	"dec_cyclic", "cyclic_visit", "inc_shared", "dec_shared",
	"free_accounted", "free_root",
	"note_alloc_static", "note_alloc_rc-local", "note_alloc_rc-shared",
	"note_alloc_cyclic", "note_alloc_pinned", "note_alloc_implicit",
}

// Розкладка збігається з runtime.CyclicDesc і runtime.CyclicEdge.
type cyclicDesc struct {
	size  uintptr
	edges []cyclicEdge
}

type cyclicEdge struct {
	off  uintptr
	elem *cyclicDesc
}

// Розкладка збігається з runtime.SharedDesc; ребер немає.
type sharedDesc struct {
	size  uintptr
	cat   int32
	edges []struct{}
}

type node struct {
	next unsafe.Pointer
	val  int
}

//go:linkname allocRoot github.com/xgo-dev/llgo/runtime/internal/runtime.AllocRoot
func allocRoot(size uintptr) unsafe.Pointer

//go:linkname freeRoot github.com/xgo-dev/llgo/runtime/internal/runtime.FreeRoot
func freeRoot(ptr unsafe.Pointer)

//go:linkname allocCounted github.com/xgo-dev/llgo/runtime/internal/runtime.AllocCounted
func allocCounted(size uintptr) unsafe.Pointer

//go:linkname inc github.com/xgo-dev/llgo/runtime/internal/runtime.Inc
func inc(ptr unsafe.Pointer)

//go:linkname decAccounted github.com/xgo-dev/llgo/runtime/internal/runtime.DecAccounted
func decAccounted(ptr unsafe.Pointer, size uintptr)

//go:linkname decCyclic github.com/xgo-dev/llgo/runtime/internal/runtime.DecCyclic
func decCyclic(ptr unsafe.Pointer, desc unsafe.Pointer)

//go:linkname incShared github.com/xgo-dev/llgo/runtime/internal/runtime.IncShared
func incShared(ptr unsafe.Pointer)

//go:linkname decShared github.com/xgo-dev/llgo/runtime/internal/runtime.DecShared
func decShared(ptr unsafe.Pointer, desc unsafe.Pointer)

//go:linkname noteAlloc github.com/xgo-dev/llgo/runtime/internal/runtime.NoteAlloc
func noteAlloc(size uintptr, cat int32)

//go:linkname freeAccounted github.com/xgo-dev/llgo/runtime/internal/runtime.FreeAccounted
func freeAccounted(ptr unsafe.Pointer, size uintptr, cat int32)

//go:linkname opCountOf github.com/xgo-dev/llgo/runtime/internal/runtime.OpCount
func opCountOf(op int32) uint64

var nodeDesc = cyclicDesc{size: unsafe.Sizeof(node{})}

var boxDesc = sharedDesc{size: 16, cat: 2}

func init() {
	nodeDesc.edges = []cyclicEdge{{off: unsafe.Offsetof(node{}.next), elem: &nodeDesc}}
}

// round робить одну ітерацію горутини категорії cat. Очікувані зміни
// лічильників — perRound.
func round(zero unsafe.Pointer, cat int32) {
	// Лічильний об'єкт: Inc і два DecAccounted, другий звільняє.
	p := allocCounted(16)
	inc(p)
	decAccounted(p, 16)
	decAccounted(p, 16)

	// Ланцюг a → b: один DecCyclic звільняє обидва, b — відвіданий вузол.
	a := allocCounted(nodeDesc.size)
	b := allocCounted(nodeDesc.size)
	(*node)(a).next = b
	(*node)(b).next = nil
	decCyclic(a, unsafe.Pointer(&nodeDesc))

	// Спільний об'єкт: IncShared і два DecShared, другий звільняє.
	s := allocCounted(boxDesc.size)
	incShared(s)
	decShared(s, unsafe.Pointer(&boxDesc))
	decShared(s, unsafe.Pointer(&boxDesc))

	freeRoot(allocRoot(32))

	noteAlloc(8, cat)
	freeAccounted(zero, 8, cat) // zerobase: облік без FreeRoot
}

func perRound(cat int32) (want [opCount]uint64) {
	want[opAlloc] = 4 // AllocCounted іде через AllocU
	want[opAllocRoot] = 1
	want[opAllocCounted] = 4
	want[opInc] = 1
	want[opDecAccounted] = 2
	want[opDecCyclic] = 1
	want[opCyclicVisit] = 1
	want[opIncShared] = 1
	want[opDecShared] = 2
	want[opFreeAccounted] = 1
	want[opFreeRoot] = 5
	want[opNoteAlloc+int(cat)] = 1
	return want
}

func take() (s [opCount]uint64) {
	for op := range s {
		s[op] = opCountOf(int32(op))
	}
	return s
}

// Бар'єри — атомарні змінні: між знімками фікстура не виділяє і не
// звільняє, тож у різницю потрапляють лише операції з циклів горутин.
var ready, finished, start, release atomic.Int32

func wait(flag *atomic.Int32, want int32) {
	for flag.Load() != want {
		runtime.Gosched()
	}
}

func main() {
	n := 100_000
	if len(os.Args) > 1 {
		var err error
		if n, err = strconv.Atoi(os.Args[1]); err != nil || n < 1 {
			fmt.Fprintln(os.Stderr, "bad n", os.Args[1])
			os.Exit(2)
		}
	}
	var want [opCount]uint64
	for g := range workers {
		for op, k := range perRound(int32(g % 6)) {
			want[op] += k * uint64(n)
		}
	}
	zero := allocCounted(0)

	var wg sync.WaitGroup
	for g := range workers {
		cat := int32(g % 6)
		wg.Go(func() {
			ready.Add(1)
			wait(&start, 1)
			for range n {
				round(zero, cat)
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

	failed := false
	for op := range opCount {
		if got := after[op] - before[op]; got != want[op] {
			fmt.Fprintln(os.Stderr, opNames[op], "delta", got, "want", want[op])
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
	println("opstat-ok", n)
}
