package corpus

import "unsafe"

func pinNew() {
	p := new(int) // want "^pinned: unsafe$"
	u := unsafe.Pointer(p)
	_ = u
}

func pinUint() {
	p := new(int) // want "^pinned: unsafe$"
	u := uintptr(unsafe.Pointer(p))
	_ = u
}

func pinSlice() {
	s := make([]byte, 8) // want "^pinned: unsafe$"
	u := unsafe.Pointer(&s[0])
	_ = u
}

func pinField() {
	s := &struct{ f int }{} // want "^pinned: unsafe$"
	u := unsafe.Pointer(&s.f)
	_ = u
}

func pinDyn(n int) {
	s := make([]int, n) // want "^pinned: unsafe$"
	u := unsafe.Pointer(unsafe.SliceData(s))
	_ = u
}

// pinned перекриває і return, і cyclic.
func pinReturn() unsafe.Pointer {
	p := new(int) // want "^pinned: unsafe$"
	return unsafe.Pointer(p)
}

func pinCycle() {
	n := &list{} // want "^pinned: unsafe$"
	u := unsafe.Pointer(n)
	_ = u
}
