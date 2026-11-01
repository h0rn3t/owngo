//go:build owngo

// Програма прийняття P9.2: IncShared і DecShared рантайму. Код 0 лише якщо
// кожен випадок звільнив рівно очікувані байти. Аргумент n — пари
// IncShared/DecShared на горутину; ланцюг має 10·n вузлів. За
// замовчуванням n = 1 000 000, для ASan — 100 000.
package main

import (
	"fmt"
	"os"
	"strconv"
	"sync"
	"unsafe"
)

// Номери збігаються з runtime.CatRCLocal … CatCyclic.
const (
	catRCLocal  int32 = 1
	catRCShared int32 = 2
	catCyclic   int32 = 3
)

// Розкладка збігається з runtime.CyclicDesc і runtime.CyclicEdge.
type cyclicDesc struct {
	size  uintptr
	edges []cyclicEdge
}

type cyclicEdge struct {
	off  uintptr
	elem *cyclicDesc
}

// Розкладка збігається з runtime.SharedDesc і runtime.SharedEdge.
type sharedDesc struct {
	size  uintptr
	cat   int32
	edges []sharedEdge
}

type sharedEdge struct {
	off    uintptr
	shared *sharedDesc
	cyclic *cyclicDesc
	size   uintptr
}

// snode — вузол спільної втечі; next — спільна дитина.
type snode struct {
	next *snode
	val  int
}

// pnode — спільний вузол з переміщеними дітьми: cyclic і rc-local.
type pnode struct {
	ring *cnode
	leaf *int
}

type cnode struct {
	next *cnode
	val  int
}

//go:linkname allocCounted github.com/xgo-dev/llgo/runtime/internal/runtime.AllocCounted
func allocCounted(size uintptr) unsafe.Pointer

//go:linkname incShared github.com/xgo-dev/llgo/runtime/internal/runtime.IncShared
func incShared(ptr unsafe.Pointer)

//go:linkname decShared github.com/xgo-dev/llgo/runtime/internal/runtime.DecShared
func decShared(ptr unsafe.Pointer, desc unsafe.Pointer)

//go:linkname noteAlloc github.com/xgo-dev/llgo/runtime/internal/runtime.NoteAlloc
func noteAlloc(size uintptr, cat int32)

//go:linkname freed github.com/xgo-dev/llgo/runtime/internal/runtime.Freed
func freed() uint64

//go:linkname live github.com/xgo-dev/llgo/runtime/internal/runtime.Live
func live(cat int32) uint64

var (
	snodeDesc = sharedDesc{size: unsafe.Sizeof(snode{}), cat: catRCShared}
	cnodeDesc = cyclicDesc{size: unsafe.Sizeof(cnode{})}
	pnodeDesc = sharedDesc{size: unsafe.Sizeof(pnode{}), cat: catRCShared}
)

func init() {
	snodeDesc.edges = []sharedEdge{{off: unsafe.Offsetof(snode{}.next), shared: &snodeDesc}}
	cnodeDesc.edges = []cyclicEdge{{off: unsafe.Offsetof(cnode{}.next), elem: &cnodeDesc}}
	pnodeDesc.edges = []sharedEdge{
		{off: unsafe.Offsetof(pnode{}.ring), cyclic: &cnodeDesc},
		{off: unsafe.Offsetof(pnode{}.leaf), size: unsafe.Sizeof(0)},
	}
}

func newSNode(val int) *snode {
	s := (*snode)(allocCounted(unsafe.Sizeof(snode{})))
	*s = snode{val: val}
	noteAlloc(unsafe.Sizeof(snode{}), catRCShared)
	return s
}

// check звіряє зміну Freed і сумарне зменшення Live після f з want.
func check(name string, want uint64, f func()) {
	before := freed()
	var liveBefore [catCyclic + 1]uint64
	for cat := catRCLocal; cat <= catCyclic; cat++ {
		liveBefore[cat] = live(cat)
	}
	f()
	if got := freed() - before; got != want {
		fmt.Fprintln(os.Stderr, name, "freed delta", got, "want", want)
		os.Exit(1)
	}
	var dropped uint64
	for cat := catRCLocal; cat <= catCyclic; cat++ {
		dropped += liveBefore[cat] - live(cat)
	}
	if dropped != want {
		fmt.Fprintln(os.Stderr, name, "live delta", dropped, "want", want)
		os.Exit(1)
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
	desc := unsafe.Pointer(&snodeDesc)

	// Вісім горутин по n пар на одному об'єкті: жодна пара не звільняє,
	// останній DecShared звільняє рівно раз.
	check("pairs", uint64(snodeDesc.size), func() {
		obj := unsafe.Pointer(newSNode(0))
		var wg sync.WaitGroup
		for range 8 {
			incShared(obj)
			wg.Go(func() {
				for range n {
					incShared(obj)
					decShared(obj, desc)
				}
				decShared(obj, desc)
			})
		}
		wg.Wait()
		decShared(obj, desc)
	})

	// Ланцюг: одиниця кожного вузла переміщена в next, один DecShared
	// у горутині з типовим стеком.
	length := 10 * n
	check("chain", uint64(length)*uint64(snodeDesc.size), func() {
		head := newSNode(0)
		tail := head
		for i := 1; i < length; i++ {
			next := newSNode(i)
			tail.next = next
			tail = next
		}
		done := make(chan struct{})
		go func() {
			decShared(unsafe.Pointer(head), desc)
			close(done)
		}()
		<-done
	})

	// Кільце двох спільних вузлів лишається живим.
	check("ring", 0, func() {
		a, b := newSNode(1), newSNode(2)
		a.next = b
		incShared(unsafe.Pointer(a))
		b.next = a
		decShared(unsafe.Pointer(a), desc)
	})

	// Переміщені діти cyclic і rc-local звільняються каскадом.
	want := uint64(pnodeDesc.size + cnodeDesc.size + unsafe.Sizeof(0))
	check("moved", want, func() {
		p := (*pnode)(allocCounted(unsafe.Sizeof(pnode{})))
		*p = pnode{}
		noteAlloc(unsafe.Sizeof(pnode{}), catRCShared)
		c := (*cnode)(allocCounted(unsafe.Sizeof(cnode{})))
		*c = cnode{val: 3}
		noteAlloc(unsafe.Sizeof(cnode{}), catCyclic)
		leaf := (*int)(allocCounted(unsafe.Sizeof(0)))
		*leaf = 4
		noteAlloc(unsafe.Sizeof(0), catRCLocal)
		p.ring, p.leaf = c, leaf
		decShared(unsafe.Pointer(p), unsafe.Pointer(&pnodeDesc))
	})

	check("zerobase", 0, func() {
		z := allocCounted(0)
		incShared(z)
		decShared(z, desc)
		decShared(z, desc)
		incShared(nil)
		decShared(nil, desc)
	})
	println("atomicrc-ok", n)
}
