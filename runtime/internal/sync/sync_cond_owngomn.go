//go:build owngo && owngomn && (linux || darwin)

package sync

import c "github.com/xgo-dev/llgo/runtime/internal/clite"

// condWait під M:N віддає логічний процесор на час очікування: інакше
// горутина в pthread_cond_wait тримала б його, і решта горутин стояла б.
func condWait(cond *Cond, mutex *Mutex) c.Int {
	BlockEnter()
	r := pthreadCondWait(cond, mutex)
	BlockExit()
	return r
}
