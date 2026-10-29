package corpus

// Друга діагностика на рядку append — тимчасовий масив аргументів, не новий буфер зрізу.
func grow() {
	s := make([]int, 1) // want "^static$"
	s = append(s, 2)    // want "^implicit: append$" "^static$"
	_ = s[0]
}

// Локальний аліас b не виводить make зі static у v0.
// Чи можна звільнити старий масив після append, вирішує v1 (P0.11).
func aliasAppend() {
	a := make([]int, 1, 1) // want "^static$"
	b := a
	a = append(a, 1) // want "^implicit: append$" "^static$"
	_ = a[0]
	_ = b[0]
}

func appendSpread(a, b []int) []int {
	return append(a, b...) // want "^implicit: append$"
}

func concat(a, b string) string {
	return a + b // want "^implicit: concat$"
}

func concatTwo(a, b, c string) string {
	s := a + b   // want "^implicit: concat$"
	return s + c // want "^implicit: concat$"
}

func toString(b []byte) string {
	return string(b) // want "^implicit: string$"
}

func toBytes(s string) []byte {
	return []byte(s) // want "^implicit: string$"
}

func toRunes(s string) []rune {
	return []rune(s) // want "^implicit: string$"
}
