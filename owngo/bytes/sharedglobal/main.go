//go:build owngo

// Програма прийняття P9.4: змінна, захоплена замиканням go, ще йде
// в глобальну змінну. Її байти лишаються серед живих; звільняються комірка
// параметра n і WaitGroup.
package main

import (
	"os"
	"runtime"
	"sync"
	"time"
	"unsafe"
)

//go:linkname ownGoFreed github.com/xgo-dev/llgo/runtime/internal/runtime.Freed
func ownGoFreed() uint64

//go:linkname ownGoLive github.com/xgo-dev/llgo/runtime/internal/runtime.Live
func ownGoLive(cat int32) uint64

const catRCShared = 2

type S struct{ a, b int }

var sink *S

func spawn(n int) int {
	var wg sync.WaitGroup
	var s S
	sink = &s
	wg.Add(1)
	go func() {
		s.a = n
		wg.Done()
	}()
	wg.Wait()
	return s.a
}

func main() {
	beforeFreed := ownGoFreed()
	beforeLive := ownGoLive(catRCShared)
	if spawn(3) != 3 {
		os.Exit(2)
	}
	want := uint64(unsafe.Sizeof(0) + unsafe.Sizeof(sync.WaitGroup{}))
	for end := time.Now().Add(5 * time.Second); ownGoFreed()-beforeFreed < want && time.Now().Before(end); {
		// time.Sleep не годиться: він звільняє свій канал, і ці байти
		// потрапили б у Freed.
		runtime.Gosched()
	}
	// Ще мить: зайве звільнення S теж мало б час статися.
	for end := time.Now().Add(10 * time.Millisecond); time.Now().Before(end); {
		runtime.Gosched()
	}
	if got := ownGoFreed() - beforeFreed; got != want {
		println("freed delta", got, "want", want)
		os.Exit(3)
	}
	if got, want := ownGoLive(catRCShared), beforeLive+uint64(unsafe.Sizeof(S{})); got != want {
		println("live rc-shared", got, "want", want)
		os.Exit(4)
	}
	if sink.a != 3 {
		os.Exit(5)
	}
	os.Exit(0)
}
