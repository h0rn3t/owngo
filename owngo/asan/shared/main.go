//go:build owngo

// Програма прийняття P3.4: купове місце rc-shared лишається живим до останнього читання.
// ASan звіту не дає.
package main

import "os"

var kept *int

func main() {
	p := new(int)
	*p = 7
	kept = p
	if kept == nil || *kept != 7 {
		os.Exit(2)
	}
	println("asan-shared-ok")
}
