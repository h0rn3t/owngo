//go:build owngo

// Package site передає int у невідомий виклик і повертає його значення.
package site

// Held передає новий int у f і повертає це значення.
func Held(f func(*int)) int {
	p := new(int)
	*p = 7
	f(p)
	return *p
}
