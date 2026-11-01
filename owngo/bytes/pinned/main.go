//go:build owngo

// Програма прийняття P10.4: закріплене місце не звільняється ні у своїй
// функції, ні через межу виклику, ні разом з батьком. Кожен випадок звіряє
// зміну owngo freed, owngo live pinned і суми живих байтів.
package main

import (
	"os"
	"unsafe"
)

//go:linkname ownGoFreed github.com/xgo-dev/llgo/runtime/internal/runtime.Freed
func ownGoFreed() uint64

//go:linkname ownGoLive github.com/xgo-dev/llgo/runtime/internal/runtime.Live
func ownGoLive(cat int32) uint64

const (
	catPinned = 4
	catCount  = 6
)

type T struct{ a, b int }

type Box struct{ p *T }

type Node struct {
	next *Node
	data *T
}

var (
	addr  uintptr
	keep  [5]unsafe.Pointer
	check int
)

func liveAll() uint64 {
	var sum uint64
	for cat := int32(0); cat < catCount; cat++ {
		sum += ownGoLive(cat)
	}
	return sum
}

// local — місце перетворюється у своїй функції.
func local() {
	t := &T{a: 1}
	keep[0] = unsafe.Pointer(t)
	check += t.a
}

func New(a int) *T { return &T{a: a} }

// callerPins — свіжий результат перетворює викликач.
func callerPins() {
	p := New(2)
	addr = uintptr(unsafe.Pointer(p))
}

func stash(p *T) { keep[2] = unsafe.Pointer(p) }

// calleePins — перетворює і зберігає викликаний.
func calleePins() {
	p := &T{a: 3}
	stash(p)
	check += p.a
}

// boxPins — закріплене місце в полі static-об'єкта: коробка звільняється.
func boxPins() {
	t := &T{a: 4}
	keep[3] = unsafe.Pointer(t)
	b := &Box{p: t}
	check += b.p.a
}

// ringPins — закріплене місце в полі кільця: кільце звільняє DecCyclic.
func ringPins() {
	t := &T{a: 5}
	keep[4] = unsafe.Pointer(t)
	a := &Node{}
	b := &Node{}
	a.next = b
	b.next = a
	a.data = t
	check += a.data.a
}

// expect повертає код виходу: 0, якщо зміни лічильників збіглися.
func expect(name string, run func(), freed, pinned, live uint64) int {
	beforeFreed, beforePinned, beforeLive := ownGoFreed(), ownGoLive(catPinned), liveAll()
	run()
	if got := ownGoFreed() - beforeFreed; got != freed {
		println(name, "freed delta", got, "want", freed)
		return 3
	}
	if got := ownGoLive(catPinned) - beforePinned; got != pinned {
		println(name, "live pinned delta", got, "want", pinned)
		return 4
	}
	if got := liveAll() - beforeLive; got != live {
		println(name, "live delta", got, "want", live)
		return 5
	}
	return 0
}

func main() {
	size := uint64(unsafe.Sizeof(T{}))
	cases := []struct {
		name                string
		run                 func()
		freed, pinned, live uint64
	}{
		{"local", local, 0, size, size},
		// Свіжий результат облікує New у своїй категорії: живий, але не pinned.
		{"callerPins", callerPins, 0, 0, size},
		{"calleePins", calleePins, 0, size, size},
		{"boxPins", boxPins, uint64(unsafe.Sizeof(Box{})), size, size},
		{"ringPins", ringPins, 2 * uint64(unsafe.Sizeof(Node{})), size, size},
	}
	for _, c := range cases {
		if code := expect(c.name, c.run, c.freed, c.pinned, c.live); code != 0 {
			os.Exit(code)
		}
	}
	got := (*T)(keep[0]).a + (*T)(unsafe.Pointer(addr)).a + (*T)(keep[2]).a + (*T)(keep[3]).a + (*T)(keep[4]).a
	if check != 1+3+4+5 || got != 1+2+3+4+5 {
		println("values", check, got)
		os.Exit(6)
	}
	os.Exit(0)
}
