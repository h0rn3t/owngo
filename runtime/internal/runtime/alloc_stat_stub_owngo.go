//go:build owngo && owngostat && (owngoasan || !(linux || darwin))

package runtime

// Без алокатора owngo (ASan чи інша ОС) рядків owngo alloc немає.
func reportAlloc() {}
