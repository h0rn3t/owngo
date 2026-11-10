//go:build owngo && owngomn

// Програма прийняття P-NEW-09.8: C кличе Go, Go блокується на каналі, а C
// після повернення відпускає свій pthread_mutex (з перевіркою власника) на
// тому самому потоці. Вісім горутин на GOMAXPROCS=4 роблять це по 50 разів:
// без прив'язки G до M callback продовжується на іншій M. Код 0 лише якщо
// кожен виклик повернувся на свій потік.
package main

/*
int run_locked(int id);
*/
import "C"

import (
	"fmt"
	"os"
	"runtime"
	"sync"
)

const (
	workers = 8
	rounds  = 50
)

var wake [workers]chan struct{}

//export goBlock
func goBlock(id C.int) {
	<-wake[id]
}

func main() {
	runtime.GOMAXPROCS(4)
	for i := range wake {
		wake[i] = make(chan struct{})
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	bad := 0
	for id := range workers {
		wg.Go(func() {
			for range rounds {
				if rc := C.run_locked(C.int(id)); rc != 0 {
					mu.Lock()
					bad++
					mu.Unlock()
				}
			}
		})
	}
	// Будимо callback-и по колу, доки кожен не отримав rounds сигналів.
	for range rounds {
		for id := range workers {
			wake[id] <- struct{}{}
		}
	}
	wg.Wait()
	if bad != 0 {
		fmt.Fprintf(os.Stderr, "mn-callback: %d of %d calls returned to C on another thread\n", bad, workers*rounds)
		os.Exit(1)
	}
	fmt.Printf("mn-callback calls %d same thread\n", workers*rounds)
}
