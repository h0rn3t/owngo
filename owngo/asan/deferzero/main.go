//go:build owngo

// Фікстура P11.3: аргумент defer нульового розміру лежить останнім полем
// вузла defer, тобто на кінці алокації (GOROOT/test recover.go,
// test11reflect2). ASan рапортує запис нульового розміру туди. Під ASan процес
// має завершитись без звіту.
package main

type empty struct{}

func (empty) m() {}

func main() {
	f := empty.m
	defer f(empty{})
}
