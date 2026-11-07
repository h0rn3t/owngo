//go:build owngo

// Програма прийняття P-NEW-03.5: канал з make звільняється разом з буфером.
// Канал static — на останньому використанні, канал, що йде в go аргументом
// чи через захоплену змінну, — тим, хто відпустить його останнім. Під ASan
// канал не читається після звільнення.
package main

import (
	"os"
	"runtime"
	"sync"
	"time"
	_ "unsafe"
)

//go:linkname ownGoLive github.com/xgo-dev/llgo/runtime/internal/runtime.Live
func ownGoLive(cat int32) uint64

//go:linkname ownGoFreed github.com/xgo-dev/llgo/runtime/internal/runtime.Freed
func ownGoFreed() uint64

const (
	catCyclic = 3
	rounds    = 20
)

//go:noinline
func local() int {
	c := make(chan int, 4)
	c <- 1
	c <- 2
	return <-c + <-c
}

//go:noinline
func borrowed() int {
	c := make(chan int, 1)
	fill(c)
	return <-c
}

//go:noinline
func fill(c chan int) { c <- 3 }

//go:noinline
func goArg() int {
	c := make(chan int)
	go worker(c)
	return <-c
}

//go:noinline
func worker(c chan int) { c <- 4 }

//go:noinline
func captured() int {
	done := make(chan struct{})
	n := 0
	go func() {
		n = 5
		close(done)
	}()
	<-done
	return n
}

//go:noinline
func buffered() int {
	c := make(chan int, 8)
	var wg sync.WaitGroup
	wg.Add(1)
	sum := 0
	go func() {
		defer wg.Done()
		for v := range c {
			sum += v
		}
	}()
	for i := range 8 {
		c <- i
	}
	close(c)
	wg.Wait()
	return sum
}

//go:noinline
func selected() int {
	c := make(chan int)
	quit := make(chan struct{})
	go func() {
		select {
		case c <- 6:
		case <-quit:
		}
	}()
	v := <-c
	close(quit)
	return v
}

func check(name string, want int, f func() int) {
	before, beforeFreed := ownGoLive(catCyclic), ownGoFreed()
	for range rounds {
		if got := f(); got != want {
			println(name, "got", got, "want", want)
			os.Exit(2)
		}
	}
	// Горутина віддає канал після повернення своєї функції. Очікування не
	// виділяє пам'яті: time.Sleep звільняє свій канал і рухав би Freed.
	end := time.Now().Add(5 * time.Second)
	for ownGoLive(catCyclic) != before && time.Now().Before(end) {
		runtime.Gosched()
	}
	if got := ownGoLive(catCyclic); got != before {
		println(name, "live cyclic", got, "want", before)
		os.Exit(3)
	}
	if ownGoFreed() == beforeFreed {
		println(name, "freed nothing")
		os.Exit(4)
	}
}

func main() {
	check("local", 3, local)
	check("borrowed", 3, borrowed)
	check("goArg", 4, goArg)
	check("captured", 5, captured)
	check("buffered", 28, buffered)
	check("selected", 6, selected)
	println("asan-chanfree-ok")
}
