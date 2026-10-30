//go:build owngo

// Package site передає один базовий масив у невідомий виклик.
package site

// Held передає новий масив із восьми байтів у f і повертає його перший байт.
func Held(f func(*[8]byte)) byte {
	p := new([8]byte)
	p[0] = 7
	f(p)
	return p[0]
}
