// Package corpus — фікстури аналізатора census.
//
// Очікування описані в TestCorpus.
package corpus

// Індекс локального масиву не бере адресу назовні: слот лишається Heap == false.
func stackArray() {
	var a [4]int // want "^stack$"
	a[0] = 1
	_ = a[0]
}

func stackStruct() {
	var s struct{ f int } // want "^stack$"
	s.f = 1
	_ = s.f
}

func stackIndexVar() {
	var a [4]int // want "^stack$"
	i := 1
	a[i] = 2
	_ = a[i]
}

// Повертається копія поля, не адреса структури.
func stackFieldRead() int {
	var s struct{ n int } // want "^stack$"
	s.n = 3
	return s.n
}

func stackLitCopy() {
	var s struct{ n int }     // want "^stack$"
	s = struct{ n int }{n: 1} // want "^stack$"
	_ = s.n
}

// defer створює блок recover, і іменований результат не піднімається в регістр.
func stackNamed() (x int) { // want "^stack$"
	defer func() {}()
	x = 1
	return
}
