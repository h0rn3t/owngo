//go:build owngo

// Програма прийняття P5.3: невідомий виклик утримує конкатенацію.
// ASan звіту не дає, об'єкт лишається живим.
package main

import "os"

func touch(s string) { _ = s[0] }

func held(a, b string, f func(string)) byte {
	s := a + b
	f(s)
	return s[0]
}

func main() {
	if held("abcd", "efgh", touch) != 'a' {
		os.Exit(2)
	}
	println("asan-imlive-ok")
}
