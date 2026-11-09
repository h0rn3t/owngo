//go:build owngo && owngomn

// Програма прийняття P-NEW-09.6: 100 000 горутин, живих одночасно, на
// GOMAXPROCS=4. Кожна чекає на каналі, доки стартують усі; потім кожна
// відповідає. Код 0 лише якщо всі відповіли, а потоків процесу в момент,
// коли живі всі, не більше за GOMAXPROCS + 4 (модель 1:1 тут упирається в
// ліміт потоків).
package main

import (
	"fmt"
	"os"
	"runtime"
	"sync"
)

func threads() int {
	ents, err := os.ReadDir("/proc/self/task")
	if err != nil {
		return -1 // немає /proc (macOS): кількість потоків не перевіряється
	}
	return len(ents)
}

func main() {
	const n, procs = 100_000, 4
	runtime.GOMAXPROCS(procs)
	var started sync.WaitGroup
	started.Add(n)
	release := make(chan struct{})
	done := make(chan int, n)
	for i := range n {
		go func() {
			started.Done()
			<-release
			done <- i
		}()
	}
	started.Wait()
	alive := runtime.NumGoroutine()
	th := threads()
	close(release)
	sum := 0
	for range n {
		sum += <-done
	}
	fmt.Printf("mn-spawn alive %d threads %d\n", alive, th)
	if sum != n*(n-1)/2 {
		println("sum", sum)
		os.Exit(2)
	}
	if alive < n {
		println("alive", alive, "want at least", n)
		os.Exit(3)
	}
	if th > procs+4 {
		println("threads", th, "want at most", procs+4)
		os.Exit(4)
	}
}
