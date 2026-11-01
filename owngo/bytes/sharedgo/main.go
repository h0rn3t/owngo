//go:build owngo

// Програма прийняття P9.4: змінна s і параметр n, захоплені замиканням go,
// звільняються, коли і функція, і горутина віддали свої посилання.
// WaitGroup лишається серед живих: Wait за зведенням не лише позичає.
package main

import (
	"os"
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

func spawn(n int) int {
	var wg sync.WaitGroup
	var s S
	wg.Add(1)
	go func() {
		s.a = n
		wg.Done()
	}()
	wg.Wait()
	s.b = n
	return s.a + s.b
}

func main() {
	beforeFreed := ownGoFreed()
	beforeLive := ownGoLive(catRCShared)
	if spawn(3) != 6 {
		os.Exit(2)
	}
	// DecShared горутини йде після wg.Done, тож чекаємо до 5 с.
	want := uint64(unsafe.Sizeof(S{}) + unsafe.Sizeof(0))
	for i := 0; i < 5000 && ownGoFreed()-beforeFreed < want; i++ {
		time.Sleep(time.Millisecond)
	}
	if got := ownGoFreed() - beforeFreed; got != want {
		println("freed delta", got, "want", want)
		os.Exit(3)
	}
	if got, want := ownGoLive(catRCShared), beforeLive+uint64(unsafe.Sizeof(sync.WaitGroup{})); got != want {
		println("live rc-shared", got, "want", want)
		os.Exit(4)
	}
	os.Exit(0)
}
