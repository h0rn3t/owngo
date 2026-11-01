package census

import (
	"testing"

	"golang.org/x/tools/go/ssa"
)

// Замикання робить місце rc-local: без нього місце static і звільняється free.
const interiorSrc = `package p

type S struct {
	a, f int
	u    func()
}

func (s *S) m() {}

var g *int

func read(p *int) int { return *p }

func write(p *int) { *p = 1 }

func field() int {
	s := new(S)
	get := func() int { return s.a }
	p := &s.f
	*p = 3
	return get() + *p
}

func nested() int {
	s := new(struct{ in S })
	get := func() int { return s.in.a }
	p := &s.in.f
	return get() + *p
}

func readArg() int {
	s := new(S)
	get := func() int { return s.a }
	n := read(&s.f)
	return get() + n
}

func array(k int) int {
	a := new([16]byte)
	get := func() int { return int(a[0]) }
	t := 0
	for _, x := range a[:k] {
		t += int(x)
	}
	return get() + t
}

func appended(src []byte, k int) int {
	b := append([]byte(nil), src...)
	get := func() int { return len(b) }
	t := 0
	for _, x := range b[:k] {
		t += int(x)
	}
	return get() + t
}

func returned() *int {
	s := new(S)
	get := func() int { return s.a }
	_ = get()
	return &s.f
}

func stored() {
	s := new(S)
	get := func() int { return s.a }
	_ = get()
	g = &s.f
}

func storedLocal() int {
	s := new(S)
	get := func() int { return s.a }
	q := new(*int)
	*q = &s.f
	return get() + **q
}

func captured() int {
	s := new(S)
	get := func() int { return s.a }
	p := &s.f
	h := func() int { return *p }
	return get() + h()
}

func unknown(f func(*int)) int {
	s := new(S)
	get := func() int { return s.a }
	f(&s.f)
	return get()
}

func written() int {
	s := new(S)
	get := func() int { return s.a }
	write(&s.f)
	return get()
}

func capturedReturned() *S {
	s := new(S)
	get := func() int { return s.a }
	_ = get()
	return s
}

// Форма flag.NewFlagSet: значення методу в полі, місце повертається.
func methodReturned() *S {
	s := new(S)
	s.u = s.m
	return s
}

func capturedPanics() {
	s := new(S)
	get := func() int { return s.a }
	_ = get()
	panic(s)
}

func viaPhi(c bool) int {
	s := new(S)
	get := func() int { return s.a }
	p := &s.a
	if c {
		p = &s.f
	}
	return get() + *p
}
`

// TestInteriorDec — P8.4: похідний вказівник, що живе лише в функції бази,
// не забирає в місця Dec.
func TestInteriorDec(t *testing.T) {
	pkg := buildOnePackage(t, "example.com/p", interiorSrc, ssa.SanityCheckFunctions|ssa.GlobalDebug)
	cats, _, decUses, _, _ := Place(ssaFuncs(pkg))
	tests := []struct {
		fn   string
		want bool
	}{
		{"field", true},
		{"nested", true},
		{"readArg", true},
		{"array", true},
		{"appended", true},
		{"returned", false},
		{"stored", false},
		{"storedLocal", false},
		{"captured", false},
		{"unknown", false},
		{"written", false},
		{"viaPhi", false},
		// Позначка closure сильніша за return і panic, але місце живе після функції.
		{"capturedReturned", false},
		{"methodReturned", false},
		{"capturedPanics", false},
	}
	for _, tt := range tests {
		site := interiorSite(t, pkg.Func(tt.fn))
		if _, got := decUses[site]; got != tt.want {
			t.Errorf("Place(%s) Dec on %v (%s) = %v, want %v", tt.fn, site, cats[site], got, tt.want)
		}
	}
}

// interiorSite повертає перше місце fn, що не є коміркою змінної:
// new(...) чи append. Похідний вказівник у прикладах бере саме його.
func interiorSite(t *testing.T, fn *ssa.Function) ssa.Instruction {
	t.Helper()
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			switch instr := instr.(type) {
			case *ssa.Alloc:
				if instr.Comment == "new" {
					return instr
				}
			case *ssa.Call:
				if ImplicitOrigin(instr) == "append" {
					return instr
				}
			}
		}
	}
	t.Fatalf("%s: no new or append site", fn.Name())
	return nil
}
