//go:build owngo

// Програма прийняття P8.7: невідомий виклик утримує похідний вказівник
// &s.f. ASan звіту не дає, об'єкт лишається живим після виклику.
package main

import "os"

type S struct{ a, f int }

var kept *int

func keep(p *int) { kept = p }

func held(f func(*int), n int) int {
	s := S{a: n, f: n * 2}
	get := func() int { return s.a }
	f(&s.f)
	return get()
}

func main() {
	if held(keep, 3) != 3 {
		os.Exit(2)
	}
	if *kept != 6 {
		os.Exit(3)
	}
	println("asan-interiorlive-ok")
}
