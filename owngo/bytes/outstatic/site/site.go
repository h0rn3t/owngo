//go:build owngo

// Package site виділяє один базовий масив поза пакетом main.
package site

// Size — розмір масиву: понад межу стека 64 КиБ (P-NEW-04.2), тож масив
// лишається в купі.
const Size = 64<<10 + 8

// Static повертає перший байт нового масиву з Size байтів.
func Static() byte {
	p := new([Size]byte)
	p[0] = 1
	return p[0]
}
