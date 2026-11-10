//go:build owngo

// Фікстура P-NEW-09.7: net/netip.Addr.String будує рядок ланцюжком append,
// частина якого йде через виклик appendDecimal, що повертає свій параметр.
// До виправлення census звільняв буфер, який той самий ланцюжок повертав.
// Під ASan процес має завершитись без звіту.
package main

import (
	"net/netip"
	"os"
)

func main() {
	a := netip.AddrFrom4([4]byte{127, 0, 0, 1})
	if s := a.String(); s != "127.0.0.1" {
		println("String", s)
		os.Exit(1)
	}
}
