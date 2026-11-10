//go:build owngo && owngomn

// Програма прийняття P-NEW-09.8: на GOMAXPROCS=1 одна горутина спить у
// C.usleep(200000), а дві інші за цей час роблять 1 000 обмінів
// небуферизованим каналом. Без маркера виклику C єдиний P спить разом з
// usleep, і обміни чекають на його кінець. Код 0 лише якщо всі обміни
// закінчились раніше за usleep.
package main

/*
#include <unistd.h>
*/
import "C"

import (
	"fmt"
	"os"
	"runtime"
	"sync/atomic"
	"time"
)

func main() {
	runtime.GOMAXPROCS(1)
	var entered, slept atomic.Bool
	done := make(chan struct{})
	go func() {
		entered.Store(true)
		C.usleep(200000)
		slept.Store(true)
		close(done)
	}()
	// Між entered і usleep точки планування немає: коли main знову
	// виконується, горутина вже в usleep.
	for !entered.Load() {
		runtime.Gosched()
	}

	start := time.Now()
	ping, pong := make(chan int), make(chan int)
	go func() {
		for v := range ping {
			pong <- v
		}
	}()
	for i := range 1000 {
		ping <- i
		if got := <-pong; got != i {
			fmt.Fprintf(os.Stderr, "mn-blockingc: exchange %d returned %d\n", i, got)
			os.Exit(1)
		}
	}
	close(ping)
	elapsed := time.Since(start)
	during := !slept.Load()
	<-done
	if !during {
		fmt.Fprintf(os.Stderr, "mn-blockingc: exchanges waited for C.usleep (%v)\n", elapsed)
		os.Exit(1)
	}
	fmt.Printf("mn-blockingc exchanges 1000 during usleep in %v\n", elapsed.Round(time.Microsecond))
}
