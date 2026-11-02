//go:build owngo

// Фікстура P11.3: купова алокація нульового розміру — спільний символ модуля
// (GOROOT/test append.go). Під ASan процес має завершитись без звіту.
package main

import "os"

type T struct {
	name             string
	result, expected any
}

var tests = []T{
	{"make a", append([]string{}, make([]string, 0)...), []string{}},
	{"make c", append([]struct{}{}, make([]struct{}, 0)...), []struct{}{}},
}

func appendLiteral() int {
	s := append([]string{}, "x")
	return len(s)
}

func emptyStruct() bool {
	p := &struct{}{}
	q := &struct{}{}
	return p == q
}

func main() {
	n := len(tests) + appendLiteral()
	if emptyStruct() {
		n++
	}
	if n < 3 {
		os.Exit(1)
	}
}
