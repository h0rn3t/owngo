//go:build owngo

// Фікстура P11.3: місце, що лежить у мапі, і сама мапа живі, доки з неї читають
// (GOROOT/test map.go). Під ASan процес має завершитись без звіту.
package main

import "os"

func lookup(i int) string {
	m := map[int][]string{}
	for j := 0; j < 3; j++ {
		as := make([]string, 2)
		as[0] = "x"
		m[j] = as
	}
	v, _ := m[i]
	return m[i][0] + v[0]
}

func rangeValue() string {
	m := map[string][]string{}
	as := make([]string, 2)
	as[0] = "y"
	m["a"] = as
	s := ""
	for _, v := range m {
		s += v[0]
	}
	return s
}

func main() {
	if lookup(1) != "xx" || rangeValue() != "y" {
		os.Exit(1)
	}
}
