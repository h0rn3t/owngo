//go:build owngo

// Фікстура P11.3: значення, яке llgo кладе в слово даних інтерфейсу без копії
// (канал, мапа, структура з одним вказівником), — не нова алокація
// (GOROOT/test cmp.go, bug514.go). Під ASan процес має завершитись без звіту.
package main

import "os"

type one struct{ p *int }

var g = 7

//go:noinline
func same(a, b any) bool { return a == b }

func main() {
	c := make(chan int, 1)
	var ic any = c
	if !same(c, ic) {
		os.Exit(1)
	}
	c <- 1
	m := map[int]int{1: 2}
	var im any = m
	m[3] = 4
	if len(im.(map[int]int)) != 2 {
		os.Exit(2)
	}
	var io any = one{&g}
	if *io.(one).p != 7 || <-c != 1 {
		os.Exit(3)
	}
}
