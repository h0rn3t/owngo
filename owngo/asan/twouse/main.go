//go:build owngo

// Програма прийняття P3.4: два живі читання одного базового місця rc-local
// в одній горутині. DecAccounted стоїть після обох, тож ASan звіту не дає.
package main

import "os"

func twoUses() int {
	p := new(int)
	*p = 3
	f := func() int { return *p }
	return f() + *p
}

func main() {
	if twoUses() != 6 {
		os.Exit(2)
	}
	println("asan-rclocal-ok")
}
