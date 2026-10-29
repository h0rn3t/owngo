package corpus

func retPtr() *int {
	p := new(int) // want "^rc-local: return$"
	return p
}

func retSlice(n int) []int {
	s := make([]int, n) // want "^rc-local: return$"
	return s
}

type wrap struct{ p *int }

func retField() *wrap {
	w := &wrap{}   // want "^rc-local: return$"
	w.p = new(int) // want "^rc-local: return$"
	return w
}

// keep нічого не зберігає, але v0 цього не бачить: аргумент виклику — це call.
func keep(*int) {}

func callConcrete() {
	p := new(int) // want "^rc-local: call$"
	keep(p)
}

type keeper interface{ keep(*int) }

func viaIface(k keeper) {
	p := new(int) // want "^rc-local: call$"
	k.keep(p)
}

func viaFunc(f func(*int)) {
	p := new(int) // want "^rc-local: call$"
	f(p)
}

func retClosure() func() int {
	p := new(int)                   // want "^rc-local: closure$" "^rc-local: closure$"
	return func() int { return *p } // want "^rc-local: return$"
}

type box struct{ n int }

func (b *box) N() int { return b.n }

// Method value захоплює отримувача так само, як замикання.
func methodValue() int {
	b := &box{} // want "^rc-local: closure$"
	m := b.N    // want "^static$"
	return m()
}

func interiorField(sink func(*int)) {
	s := &struct{ f int }{f: 1} // want "^rc-local: call$"
	sink(&s.f)
}

func interiorSlice(sink func([]int), i int) {
	a := make([]int, 4) // want "^rc-local: call$"
	sink(a[:i])
}

// Зріз із масиву в go/ssa одразу позначає масив як такий, що втікає.
func interiorArray(sink func([]int), i int) {
	var a [4]int // want "^rc-local: call$"
	sink(a[:i])
}
