//go:build owngo

package runtime

import (
	"unsafe"

	c "github.com/xgo-dev/llgo/runtime/internal/clite"
)

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

// FreeChan звільняє канал без байтів буфера, створений make, до якого
// більше ніхто не звернеться (канал time.Sleep). Lock і Unlock чекають,
// поки відправник, що віддав значення, вийде з критичної секції каналу.
// Канал з байтами буфера тут не звільняється: буфер AllocUAligned до free
// не годиться.
func FreeChan(p *Chan) {
	if p.elemsize != 0 && p.dataqsiz != 0 {
		panic("runtime: FreeChan of a channel with a buffer")
	}
	p.mutex.Lock()
	p.mutex.Unlock()
	FreeAccounted(unsafe.Pointer(p), unsafe.Sizeof(Chan{}), CatCyclic)
}
