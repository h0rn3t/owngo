package v1

// v1 замінює причину call зведенням параметра.
// Позичання не є втечею. Переміщення — отримувач зберігає, відправник після
// виклику вказівник не читає. Спільне — зберігають обидва, або набір цілей
// невідомий (невідоме ніколи не є позичанням). go лишається go.

func keep(*int) {}

func callKeep() {
	p := new(int) // want "^static$"
	keep(p)
}

var held *int

func stash(p *int) { held = p }

func callStash() {
	p := new(int) // want "^rc-shared: move$"
	stash(p)
}

func callShare() {
	p := new(int) // want "^rc-shared: shared$"
	stash(p)
	_ = *p
}

func id(p *int) *int { return p }

func callID() {
	p := new(int) // want "^static$"
	id(p)
}

func callRet() *int {
	p := new(int) // want "^rc-local: return$"
	return id(p)
}

type wrap struct{ p *int }

func box(p *int) *wrap {
	w := &wrap{} // want "^rc-local: return$"
	w.p = p
	return w
}

func callBox() *wrap {
	p := new(int) // want "^rc-local: return$"
	return box(p)
}

func callBoxDrop() {
	p := new(int) // want "^static$"
	box(p)
}

type keeper interface{ keep(*int) }

func callIface(k keeper) {
	p := new(int) // want "^rc-local: shared$"
	k.keep(p)
}

func callFunc(f func(*int)) {
	p := new(int) // want "^rc-local: shared$"
	f(p)
}

type calm struct{}

func (*calm) keep(*int) {}

func callLocal() {
	var k keeper = &calm{} // want "^static$"
	p := new(int)          // want "^static$"
	k.keep(p)
}

type hot struct{}

func (*hot) keep(p *int) { held = p }

func callHot() {
	var k keeper = &hot{} // want "^static$"
	p := new(int)         // want "^rc-shared: move$"
	k.keep(p)
}

func callInterior() {
	s := &struct{ f int }{f: 1} // want "^static$"
	keep(&s.f)
}

func callInteriorStash() {
	s := &struct{ f int }{f: 1} // want "^rc-shared: move$"
	stash(&s.f)
}

func keepSlice([]int) {}

func callSlice(i int) {
	a := make([]int, 4) // want "^static$"
	keepSlice(a[:i])
}

func callArray(i int) {
	var a [4]int // want "^static$"
	keepSlice(a[:i])
}

func callSinkSlice(sink func([]int), i int) {
	a := make([]int, 4) // want "^rc-local: shared$"
	sink(a[:i])
}

type boxT struct{}

func (*boxT) keep(*int) {}

func callMethod() {
	b := &boxT{}  // want "^static$"
	p := new(int) // want "^static$"
	b.keep(p)
}

func callLocalFn() {
	f := keep
	p := new(int) // want "^static$"
	f(p)
}

func rec(p *int, n int) {
	if n == 0 {
		return
	}
	rec(p, n-1)
}

func callRec() {
	p := new(int) // want "^static$"
	rec(p, 3)
}

var rg *int

func recStash(p *int, n int) {
	if n == 0 {
		rg = p
		return
	}
	recStash(p, n-1)
}

func callRecStash() {
	p := new(int) // want "^rc-shared: move$"
	recStash(p, 2)
}

func callDefer() {
	p := new(int) // want "^static$"
	defer keep(p)
}

func callDeferStash() {
	p := new(int) // want "^rc-shared: move$"
	defer stash(p)
}

func callGo() {
	p := new(int) // want "^rc-shared: go$" "^rc-shared: go$"
	go func() {   // want "^rc-shared: go$"
		_ = *p
	}()
}

func callGrow() {
	s := make([]int, 1) // want "^static$"
	s = append(s, 2)    // want "^static: append$" "^static$"
	_ = s[0]
}

func callAlias() {
	a := make([]int, 1) // want "^rc-local: shared$"
	b := a
	a = append(a, 1) // want "^rc-local: shared$" "^static$"
	_ = a[0]
	_ = b[0]
}

func callAliasSlice() {
	a := make([]int, 1) // want "^rc-local: shared$"
	b := a[:]
	a = append(a, 1) // want "^rc-local: shared$" "^static$"
	_ = b[0]
}
