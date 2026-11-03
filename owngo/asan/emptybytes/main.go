//go:build owngo

// Фікстура P11.3: []byte і []rune з порожнього рядка рантайм повертає зі
// спільного символу нульового розміру свого модуля, а не з купи
// (GOROOT/test bug333.go, issue5704.go, issue23814.go). Під ASan процес має
// завершитись без звіту.
package main

import "os"

func main() {
	s := os.Getenv("OWNGO_EMPTYBYTES_UNSET")
	b := []byte(s)
	r := []rune(s)
	if len(b) != 0 || len(r) != 0 {
		os.Exit(1)
	}
}
