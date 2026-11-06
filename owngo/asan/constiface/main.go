//go:build owngo

// Програма прийняття P-NEW-04.4: стала в інтерфейсі місця static бере слово
// даних з незмінної глобальної змінної: порівняння і switch дають ті самі
// відповіді, а free на цій адресі ASan назвав би attempting free. Стала, що
// тікає через return, і змінна в інтерфейсі лишаються в купі. Розміщення
// перевіряє TestOwnGoConstIface: FreeAccounted місця MakeInterface має розмір
// 0, тож Freed його не бачить.
// Під -asan — код 0 без тексту ASan.
package main

import (
	"errors"
	"os"
	"syscall"
)

func isEINTR(err error) bool { return err == syscall.EINTR }

func isBoom(v any) bool { return v == "boom" }

func kind(v any) int {
	switch v {
	case 1.5:
		return 1
	case "x":
		return 2
	case syscall.EPIPE:
		return 3
	}
	return 0
}

//go:noinline
func retConst() error { return syscall.EAGAIN }

//go:noinline
func boxVar(x int, v any) bool { return v == any(x) }

func main() {
	hits := 0
	for i := range 100 {
		var err error = syscall.Errno(i % 8)
		if isEINTR(err) {
			hits++
		}
		if isBoom("boom") && !isBoom("bang") && !isBoom(4) {
			hits++
		}
		hits += kind(1.5) + kind("x") + kind(syscall.EPIPE) + kind(2)
	}
	if hits != 12+100+600 {
		println("hits", hits)
		os.Exit(2)
	}
	if err := retConst(); !errors.Is(err, syscall.EAGAIN) || err.Error() != syscall.EAGAIN.Error() {
		println("retConst", err)
		os.Exit(4)
	}
	if !boxVar(7, 7) || boxVar(7, 8) {
		println("boxVar")
		os.Exit(5)
	}
	println("asan-constiface-ok")
}
