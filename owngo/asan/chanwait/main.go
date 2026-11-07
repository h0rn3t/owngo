//go:build owngo

// Програма прийняття P-NEW-03.4: блокувальні відправка й прийом і time.Sleep
// не лишають живих байтів cyclic. До зміни кожна блокувальна операція лишала
// вузол очікування (168 байтів), а кожен Sleep — ще й свій канал. Під ASan
// вузол і канал не читаються після звільнення.
package main

import (
	"os"
	"time"
	_ "unsafe"
)

//go:linkname ownGoLive github.com/xgo-dev/llgo/runtime/internal/runtime.Live
func ownGoLive(cat int32) uint64

const (
	catCyclic = 3
	rounds    = 50
)

func main() {
	ping, pong := make(chan int), make(chan int)
	go func() {
		for i := range rounds {
			time.Sleep(time.Millisecond) // прийом у main блокується
			ping <- i
			<-pong // блокується, поки main спить
		}
	}()
	before := ownGoLive(catCyclic)
	for i := range rounds {
		if got := <-ping; got != i {
			os.Exit(2)
		}
		time.Sleep(time.Millisecond) // відправник у горутині блокується
		pong <- i
	}
	if got := ownGoLive(catCyclic); got != before {
		println("live cyclic", got, "want", before)
		os.Exit(3)
	}
	println("asan-chanwait-ok")
}
