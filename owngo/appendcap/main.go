//go:build owngo

// Фікстура P11.3: append, буфер якого owngo виділяє сам, дає ту саму
// ємність, що growslice, і панікує на довжині поза діапазоном
// (GOROOT/test fixedbugs/issue41239.go, fixedbugs/issue7550.go).
// llgo run -tags owngo має завершитись кодом 0.
package main

import "os"

func overflow() (panicked bool) {
	defer func() { panicked = recover() != nil }()
	length := int(^uint(0) >> 1)
	a := make([]struct{}, length)
	b := make([]struct{}, length)
	_ = append(a, b...)
	return false
}

func main() {
	const N = 1024
	var a [N]int
	x := cap(append(a[:N-1:N], 9, 9))
	y := cap(append(a[:N:N], 9))
	if x != y {
		os.Exit(1)
	}
	if !overflow() {
		os.Exit(2)
	}
}
