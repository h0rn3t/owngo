//go:build owngo && owngomn && linux

// Програма прийняття P-NEW-09.6: runtime.LockOSThread тримає горутину на
// її потоці ОС. На GOMAXPROCS=2 прив'язана горутина 1 000 разів блокується
// на каналі, поки 8 інших горутин працюють; щоразу після пробудження
// gettid той самий. Головна горутина теж лишається на своєму потоці.
package main

import (
	"os"
	"runtime"
	"sync"
	"syscall"
)

func main() {
	runtime.GOMAXPROCS(2)
	mainTid := syscall.Gettid()
	stop := make(chan struct{})
	var busy sync.WaitGroup
	for range 8 {
		busy.Add(1)
		go func() {
			defer busy.Done()
			for {
				select {
				case <-stop:
					return
				default:
					runtime.Gosched()
				}
			}
		}()
	}
	tick := make(chan int)
	moved := make(chan int, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		tid := syscall.Gettid()
		for i := range 1000 {
			if <-tick != i || syscall.Gettid() != tid {
				moved <- i
				return
			}
		}
		moved <- -1
	}()
	for i := range 1000 {
		tick <- i
		if syscall.Gettid() != mainTid {
			println("main moved at", i)
			os.Exit(2)
		}
	}
	if at := <-moved; at != -1 {
		println("locked goroutine moved at", at)
		os.Exit(3)
	}
	close(stop)
	busy.Wait()
}
