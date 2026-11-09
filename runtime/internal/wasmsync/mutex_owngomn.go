//go:build owngo && owngomn && (linux || darwin)

package wasmsync

import (
	_ "unsafe"

	c "github.com/xgo-dev/llgo/runtime/internal/clite"
	"github.com/xgo-dev/llgo/runtime/internal/sync/atomic"
)

// Нативна версія для планувальника M:N (P-NEW-09.6): канали, семафори і
// notifyList беруть той самий код, що й wasm workers. Тримач замка не
// паркується, тож очікувач спить на futex (owngo_mn_wait у
// runtime/internal/runtime/_wrap/owngo_mn.c), а yield не потрібен.

//go:linkname mnWait C.owngo_mn_wait
func mnWait(addr *uint32, val uint32, ns int64) c.Int

//go:linkname mnWake C.owngo_mn_wake
func mnWake(addr *uint32, all c.Int)

// Mutex — замок на futex: 0 — вільний, 1 — зайнятий, 2 — є очікувачі.
type Mutex struct {
	state uint32
}

// Lock бере m; yield не кличеться.
func (m *Mutex) Lock(yield func()) {
	if _, ok := atomic.CompareAndExchange(&m.state, 0, 1); ok {
		return
	}
	for i := 0; i < 100; i++ {
		if atomic.Load(&m.state) == 0 {
			if _, ok := atomic.CompareAndExchange(&m.state, 0, 1); ok {
				return
			}
		}
	}
	for atomic.Exchange(&m.state, 2) != 0 {
		mnWait(&m.state, 2, -1)
	}
}

// Unlock звільняє m і будить одного очікувача, якщо він був.
func (m *Mutex) Unlock() {
	if atomic.Exchange(&m.state, 0) == 2 {
		mnWake(&m.state, 0)
	}
}

// Contended — чи чекає хтось на m.
func (m *Mutex) Contended() bool { return atomic.Load(&m.state) == 2 }
