//go:build owngo

// Програма прийняття P4.3: невідомий виклик утримує об'єкт поза main.
// ASan звіту не дає, об'єкт лишається живим.
package main

import (
	"os"

	"github.com/xgo-dev/llgo/owngo/asan/outlive/site"
)

func touch(p *int) { _ = *p }

func main() {
	if site.Held(touch) != 7 {
		os.Exit(2)
	}
	println("asan-outlive-ok")
}
