//go:build owngo

// Package site виділяє один базовий масив поза пакетом main.
package site

// Static повертає перший байт нового масиву з восьми байтів.
func Static() byte {
	p := new([8]byte)
	p[0] = 1
	return p[0]
}
