//go:build owngo && owngostat

// Програма прийняття P-NEW-09.3: твердження типу до інтерфейсу не виділяє
// пам'ять рантайму. Тип з багатьма методами n разів перевіряється на
// інтерфейс, який реалізує, і на той, якого не реалізує; код 0 лише якщо
// owngo ops alloc, owngo freed і сума owngo live не змінились.
package main

import (
	"os"
	"strconv"
	_ "unsafe"
)

//go:linkname opCount github.com/xgo-dev/llgo/runtime/internal/runtime.OpCount
func opCount(op int32) uint64

//go:linkname ownGoFreed github.com/xgo-dev/llgo/runtime/internal/runtime.Freed
func ownGoFreed() uint64

//go:linkname ownGoLive github.com/xgo-dev/llgo/runtime/internal/runtime.Live
func ownGoLive(cat int32) uint64

const (
	opAlloc  = 0 // opAlloc у runtime/internal/runtime
	catCount = 7 // accountCats у runtime/internal/runtime
)

// conn має стільки методів, як типовий драйвер database/sql: таблиця методів
// займає кілька сотень байтів.
type conn struct{ n int }

func (c *conn) A() int { return c.n + 1 }
func (c *conn) B() int { return c.n + 2 }
func (c *conn) C() int { return c.n + 3 }
func (c *conn) D() int { return c.n + 4 }
func (c *conn) E() int { return c.n + 5 }
func (c *conn) F() int { return c.n + 6 }
func (c *conn) G() int { return c.n + 7 }
func (c *conn) H() int { return c.n + 8 }
func (c *conn) I() int { return c.n + 9 }
func (c *conn) J() int { return c.n + 10 }
func (c *conn) K() int { return c.n + 11 }
func (c *conn) L() int { return c.n + 12 }

type hasA interface{ A() int }

type hasZ interface{ Z() int }

var sink any = &conn{n: 1}

//go:noinline
func assert(n int) (yes, no int) {
	for range n {
		if a, ok := sink.(hasA); ok {
			yes += a.A()
		}
		if _, ok := sink.(hasZ); ok {
			no++
		}
	}
	return yes, no
}

func liveAll() uint64 {
	var sum uint64
	for cat := int32(0); cat < catCount; cat++ {
		sum += ownGoLive(cat)
	}
	return sum
}

func main() {
	n := 1000
	if len(os.Args) > 1 {
		n, _ = strconv.Atoi(os.Args[1])
	}
	// Перший прохід кладе itab hasA у кеш рантайму.
	assert(1)
	allocs, freed, live := opCount(opAlloc), ownGoFreed(), liveAll()
	yes, no := assert(n)
	if yes != 2*n || no != 0 {
		println("results", yes, no)
		os.Exit(2)
	}
	if got := opCount(opAlloc) - allocs; got != 0 {
		println("alloc delta", got, "want 0")
		os.Exit(3)
	}
	if got := ownGoFreed() - freed; got != 0 {
		println("freed delta", got, "want 0")
		os.Exit(4)
	}
	if got := liveAll() - live; got != 0 {
		println("live delta", got, "want 0")
		os.Exit(5)
	}
}
