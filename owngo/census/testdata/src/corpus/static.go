package corpus

// new у go/ssa завжди Heap == true, навіть без втечі. Це static, не stack.
func staticNew() {
	p := new(int) // want "^static$"
	*p = 1
	_ = *p
}

// Стала ємність знижується до new([n]T), теж Heap == true.
func staticMakeConst() {
	s := make([]int, 3) // want "^static$"
	s[0] = 1
	_ = s[0]
}

func staticMakeDyn(n int) {
	s := make([]int, n) // want "^static$"
	if len(s) > 0 {
		s[0] = 1
	}
	_ = s
}

func staticMap() {
	m := make(map[string]int) // want "^static$"
	m["a"] = 1
	_ = m["a"]
}

func staticChan() {
	ch := make(chan int, 1) // want "^static$"
	ch <- 1
	<-ch
}

func staticBox() {
	i := any(42) // want "^static$"
	_ = i
}

// Запис у локальну структуру, яка сама не втікає, не виводить new зі static.
func staticFieldStore() {
	var w struct{ p *int } // want "^stack$"
	w.p = new(int)         // want "^static$"
	_ = *w.p
}

// Об'єкт замикання не залишає функцію. Захоплена змінна класифікується як closure.
func staticClosure() int {
	n := 1                       // want "^rc-local: closure$"
	f := func() int { return n } // want "^static$"
	return f()
}
