//go:build owngo

// Програма прийняття P1.4: місця static звільняються, об'єкт rc-local доживає
// до виходу. Під ASan процес має завершитись без звіту.
package main

import "os"

func staticNew() int {
	p := new(int)
	*p = 1
	return *p
}

func staticMakeConst() int {
	s := make([]int, 3)
	s[0] = 1
	return s[0]
}

func staticMakeDyn(n int) int {
	s := make([]int, n)
	if len(s) == 0 {
		return 0
	}
	s[0] = 1
	return s[0]
}

func staticMap() int {
	m := make(map[string]int)
	m["a"] = 1
	return m["a"]
}

func staticChan() int {
	ch := make(chan int, 1)
	ch <- 1
	return <-ch
}

func staticBox() int {
	i := any(42)
	return i.(int)
}

func staticFieldStore() int {
	var w struct{ p *int }
	w.p = new(int)
	*w.p = 1
	return *w.p
}

func staticClosure() int {
	n := 1
	f := func() int { return n }
	return f()
}

func escaped() *int {
	p := new(int)
	*p = 7
	return p
}

func main() {
	if staticNew() != 1 || staticMakeConst() != 1 || staticMakeDyn(2) != 1 {
		os.Exit(2)
	}
	if staticMap() != 1 || staticChan() != 1 || staticBox() != 42 || staticFieldStore() != 1 {
		os.Exit(3)
	}
	var stack int
	stack = 1
	live := escaped()
	if staticClosure() != 1 || stack != 1 || live == nil || *live != 7 {
		os.Exit(4)
	}
	println("asan-clean-ok")
}
