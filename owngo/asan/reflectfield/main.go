//go:build owngo

// Фікстура P11.3: reflect.Value тримає коробку інтерфейсу, у яку ValueOf
// поклав значення; Field дає Value всередину тієї самої коробки
// (GOROOT/test issue31546.go, issue19113.go, issue15528.go). Під ASan процес
// має завершитись без звіту.
package main

import (
	"os"
	"reflect"
)

var x = struct{ a, _, c int }{1, 2, 3}

func main() {
	if i := reflect.ValueOf(x).Field(2).Int(); i != 3 {
		os.Exit(1)
	}
}
