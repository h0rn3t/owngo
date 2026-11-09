//go:build owngo && owngomn

// Програма прийняття P-NEW-09.6: 10⁶ обмінів небуферизованим каналом між
// двома горутинами на GOMAXPROCS=1 і на GOMAXPROCS=4. Код 0 лише якщо
// кожна сторона отримала всі значення по порядку; час обміну — рядок
// mn-pingpong-ns.
package main

import (
	"fmt"
	"os"
	"runtime"
	"time"
)

func pingpong(rounds int) bool {
	ping, pong := make(chan int), make(chan int)
	ok := make(chan bool)
	go func() {
		for i := range rounds {
			if <-ping != i {
				ok <- false
				return
			}
			pong <- i
		}
		ok <- true
	}()
	for i := range rounds {
		ping <- i
		if <-pong != i {
			return false
		}
	}
	return <-ok
}

func main() {
	const rounds = 1_000_000
	for _, procs := range []int{1, 4} {
		runtime.GOMAXPROCS(procs)
		start := time.Now()
		if !pingpong(rounds) {
			println("pingpong failed, GOMAXPROCS", procs)
			os.Exit(2)
		}
		fmt.Printf("mn-pingpong-ns procs=%d %.1f\n", procs, float64(time.Since(start).Nanoseconds())/rounds)
	}
}
