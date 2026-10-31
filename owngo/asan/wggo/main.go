//go:build owngo

// Програма прийняття P7.1: замикання, передане в wg.Go, читає захоплений
// об'єкт уже в горутині. ASan звіту не дає, об'єкт живий до кінця горутини.
package main

import (
	"os"
	"sync"
	"time"
)

func spawn(wg *sync.WaitGroup, out *int) {
	p := new([4]int)
	p[0] = 7
	wg.Go(func() {
		time.Sleep(10 * time.Millisecond)
		*out = p[0]
	})
}

func main() {
	var wg sync.WaitGroup
	var got int
	spawn(&wg, &got)
	wg.Wait()
	if got != 7 {
		os.Exit(2)
	}
	println("asan-wggo-ok")
}
