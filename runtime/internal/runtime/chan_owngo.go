//go:build owngo

package runtime

import (
	"unsafe"

	c "github.com/xgo-dev/llgo/runtime/internal/clite"
	"github.com/xgo-dev/llgo/runtime/internal/sync/atomic"
)

// allocChan виділяє заголовок і буфер каналу одним блоком зі словом
// лічильника, як AllocCounted: слово з одиницею належить тому, хто зробив
// make, а ChanDec звільняє блок цілим. Байти блока облікуються в cyclic, як
// раніше заголовок. Лічильників операцій alloc_counted і note_alloc тут
// немає: їх рахує місце компілятора, а make каналу такого місця не має.
func allocChan(mem uintptr, cap, eltAlign int) *Chan {
	if cap > 0 && (eltAlign <= 0 || eltAlign&(eltAlign-1) != 0) {
		panic("runtime: invalid allocation alignment")
	}
	hdr := unsafe.Sizeof(Chan{})
	size := hdr
	if mem > 0 {
		size += uintptr(eltAlign) - 1 + mem
	}
	word := unsafe.Sizeof(uintptr(0))
	block := AllocU(size + word)
	*(*uintptr)(block) = 1
	p := unsafe.Add(block, word)
	c.Memset(p, 0, hdr)
	ch := (*Chan)(p)
	if cap > 0 {
		ch.buf = unsafe.Pointer(&zerobase)
		if mem > 0 {
			align := uintptr(eltAlign) - 1
			ch.buf = unsafe.Add(p, (uintptr(p)+hdr+align)&^align-uintptr(p))
		}
	}
	atomic.Add(&myAccountShard().live[CatCyclic], uint64(size))
	return ch
}

// chanSize повертає байти блока allocChan.
func chanSize(ch *Chan) uintptr {
	mem := uintptr(ch.elemsize) * uintptr(ch.dataqsiz)
	if ch.buf == nil || mem == 0 {
		return unsafe.Sizeof(Chan{})
	}
	return uintptr(ch.buf) - uintptr(unsafe.Pointer(ch)) + mem
}

// ChanDec віддає слово каналу з make на місці компілятора: static — на
// останньому живому шляху, спільного — у функції й у горутині.
func ChanDec(p unsafe.Pointer) {
	countOp(opDecChan)
	chanDec(p)
}

// FreeChan віддає слово каналу, який створив сам рантайм (канал
// time.Sleep), без лічильника операцій: місця компілятора тут немає.
func FreeChan(p *Chan) {
	chanDec(unsafe.Pointer(p))
}

// chanDec атомарно зменшує слово каналу і на нулі звільняє його блок. Lock
// і Unlock чекають, поки той, хто останнім торкався каналу в іншій
// горутині, вийде з його критичної секції. nil нічого не робить.
func chanDec(p unsafe.Pointer) {
	if p == nil {
		return
	}
	if atomic.Sub(countWord(p), 1) != 1 {
		return
	}
	ch := (*Chan)(p)
	ch.mutex.Lock()
	ch.mutex.Unlock()
	size := chanSize(ch)
	FreeRoot(unsafe.Pointer(countWord(p)))
	noteFree(size, CatCyclic)
}

// allocChanWaiter виділяє вузол очікування поза обліком, як newSelectWaiter:
// без трасувального збирача його звільняє freeChanWaiter.
func allocChanWaiter() *chanWaiter {
	w := (*chanWaiter)(c.Malloc(unsafe.Sizeof(chanWaiter{})))
	if w == nil {
		panic("out of memory")
	}
	c.Memset(unsafe.Pointer(w), 0, unsafe.Sizeof(chanWaiter{}))
	return w
}

// freeChanWaiter звільняє вузол після wait: будитель знімає його з черги
// раніше за finish, а finish тримає signal до ready, тож після wait вузла
// ніхто не торкається.
func freeChanWaiter(w *chanWaiter) {
	c.Free(unsafe.Pointer(w))
}
