package cl

import (
	"strings"
	"testing"

	"github.com/xgo-dev/llvm"
)

// TestOwnGoStaticFree — FileCheck-еквівалент P1.3: виклик runtime.Free є у
// функції з місцем static і відсутній у функції з місцем rc-local.
func TestOwnGoStaticFree(t *testing.T) {
	const src = `package foo

import "fmt"

func staticNew() int {
	p := new(int)
	*p = 1
	return *p
}

func staticJoin(cond bool) int {
	p := new(int)
	if cond {
		*p = 1
	} else {
		*p = 2
	}
	return *p
}

func escaped() *int {
	p := new(int)
	return p
}

func stacked() int {
	var x int
	x = 1
	return x
}

func printed() {
	p := new(int)
	fmt.Println(p)
}
`
	mod := compileOwnGoFree(t, src, true)
	if err := llvm.VerifyModule(mod, llvm.ReturnStatusAction); err != nil {
		t.Fatal(err)
	}
	frees := map[string]int{
		"foo.staticNew":  1,
		"foo.staticJoin": 1,
		"foo.escaped":    0,
		"foo.stacked":    0,
		"foo.printed":    0,
	}
	for name, want := range frees {
		got := strings.Count(mustNamedFunction(t, mod, name).String(), "runtime.Free")
		if got != want {
			t.Errorf("%s runtime.Free count = %d, want %d", name, got, want)
		}
	}

	off := compileOwnGoFree(t, src, false)
	if got := strings.Count(mustNamedFunction(t, off, "foo.staticNew").String(), "runtime.Free"); got != 0 {
		t.Errorf("OwnGoFree false: foo.staticNew runtime.Free count = %d, want 0", got)
	}
}

func TestOwnGoFreeBranchAllocDominates(t *testing.T) {
	const src = `package foo

func branch(cond bool) int {
	var p *int
	if cond {
		p = new(int)
		*p = 1
	} else {
		p = new(int)
		*p = 2
	}
	return *p
}
`
	mod := compileOwnGoFree(t, src, true)
	if err := llvm.VerifyModule(mod, llvm.ReturnStatusAction); err != nil {
		t.Fatal(err)
	}
}

func compileOwnGoFree(t *testing.T, src string, free bool) llvm.Module {
	t.Helper()
	ssaPkg, _, files := buildGoSSAPkg(t, src)
	prog := newLLSSAProg(t)
	t.Cleanup(prog.Dispose)
	pkg, _, err := NewPackageExWithEmbedMetaOptions(
		prog, nil, nil, nil, ssaPkg, files, nil, false,
		Options{OwnGoFree: free},
	)
	if err != nil {
		t.Fatal(err)
	}
	return pkg.Module()
}

// TestOwnGoFreeBytes — P1.5: місце static отримує облік розміру і
// FreeAccounted, місце rc-local лише облік, стек не обліковується.
func TestOwnGoFreeBytes(t *testing.T) {
	const src = `package foo

func staticNew() int {
	p := new([8]byte)
	p[0] = 1
	return int(p[0])
}

func escaped() *[8]byte {
	p := new([8]byte)
	p[0] = 7
	return p
}

func stacked() int {
	var x [8]byte
	x[0] = 1
	return int(x[0])
}
`
	mod := compileOwnGoFree(t, src, true)
	if err := llvm.VerifyModule(mod, llvm.ReturnStatusAction); err != nil {
		t.Fatal(err)
	}
	static := mustNamedFunction(t, mod, "foo.staticNew").String()
	if !strings.Contains(static, "runtime.NoteAlloc") {
		t.Errorf("foo.staticNew IR = missing runtime.NoteAlloc, want a call")
	}
	if !strings.Contains(static, "runtime.FreeAccounted") {
		t.Errorf("foo.staticNew IR = missing runtime.FreeAccounted, want a call")
	}
	escaped := mustNamedFunction(t, mod, "foo.escaped").String()
	if !strings.Contains(escaped, "runtime.NoteAlloc") {
		t.Errorf("foo.escaped IR = missing runtime.NoteAlloc, want a call")
	}
	if strings.Contains(escaped, "runtime.FreeAccounted") {
		t.Errorf("foo.escaped IR contains runtime.FreeAccounted, want none")
	}
	stacked := mustNamedFunction(t, mod, "foo.stacked").String()
	if strings.Contains(stacked, "runtime.NoteAlloc") || strings.Contains(stacked, "runtime.FreeAccounted") {
		t.Errorf("foo.stacked IR accounted a stack allocation, want neither call")
	}

	off := compileOwnGoFree(t, src, false)
	if got := mustNamedFunction(t, off, "foo.staticNew").String(); strings.Contains(got, "runtime.NoteAlloc") || strings.Contains(got, "runtime.FreeAccounted") {
		t.Errorf("OwnGoFree false: foo.staticNew IR accounted an allocation, want neither call")
	}
}

func TestOwnGoFreeStoredPointerStaysLive(t *testing.T) {
	const src = `package foo

type extra struct{ next *int }
type head struct{ extra *extra }

func grow(h *head) {
	if h.extra == nil {
		h.extra = new(extra)
	}
	h.extra.next = new(int)
}
`
	mod := compileOwnGoFree(t, src, true)
	if err := llvm.VerifyModule(mod, llvm.ReturnStatusAction); err != nil {
		t.Fatal(err)
	}
	got := strings.Count(mustNamedFunction(t, mod, "foo.grow").String(), "runtime.Free")
	if got != 0 {
		t.Errorf("foo.grow runtime.Free count = %d, want 0", got)
	}
}

func TestOwnGoRCLocal(t *testing.T) {
	const src = `package foo

var g *int

func closureUse() int {
	p := new(int)
	*p = 1
	f := func() int { return *p }
	return f() + *p
}

func shared() int {
	p := new(int)
	*p = 1
	g = p
	return *p
}

func unknown(f func(*int)) int {
	p := new(int)
	*p = 1
	f(p)
	return *p
}

func staticNew() int {
	p := new(int)
	*p = 1
	return *p
}

func interior(sink func(*int)) int {
	s := &struct{ f int }{f: 1}
	sink(&s.f)
	return s.f
}
`
	mod := compileOwnGoFree(t, src, true)
	if err := llvm.VerifyModule(mod, llvm.ReturnStatusAction); err != nil {
		t.Fatal(err)
	}
	base := mustNamedFunction(t, mod, "foo.closureUse").String()
	// Замикання захоплює змінну: SSA тримає два купові Alloc, слот і new(int).
	if got := strings.Count(base, "runtime.DecAccounted"); got != 2 {
		t.Errorf("foo.closureUse runtime.DecAccounted count = %d, want 2", got)
	}
	if !strings.Contains(base, "runtime.AllocCounted") {
		t.Errorf("foo.closureUse IR = missing runtime.AllocCounted, want a call")
	}
	for _, name := range []string{"foo.shared", "foo.unknown", "foo.staticNew", "foo.interior"} {
		body := mustNamedFunction(t, mod, name).String()
		if got := strings.Count(body, "runtime.DecAccounted"); got != 0 {
			t.Errorf("%s runtime.DecAccounted count = %d, want 0", name, got)
		}
	}
	static := mustNamedFunction(t, mod, "foo.staticNew").String()
	if !strings.Contains(static, "runtime.FreeAccounted") {
		t.Errorf("foo.staticNew IR = missing runtime.FreeAccounted, want a call")
	}
	off := compileOwnGoFree(t, src, false)
	if got := strings.Count(mustNamedFunction(t, off, "foo.closureUse").String(), "runtime.DecAccounted"); got != 0 {
		t.Errorf("OwnGoFree false: foo.closureUse runtime.DecAccounted count = %d, want 0", got)
	}
}

// TestOwnGoImplicit — P5.1: доведене місце append, конкатенації або
// перетворення рядка отримує той самий облік, що й володіння v1.
func TestOwnGoImplicit(t *testing.T) {
	const src = `package foo

func staticConcat(a, b string) int {
	s := a + b
	return len(s)
}

func staticAppend(s, extra []int) int {
	s = append(s, extra...)
	return len(s)
}

func staticConvert(b []byte) int {
	s := string(b)
	return len(s)
}

func aliasAppend(extra []int) int {
	a := make([]int, 1, 8)
	b := a
	a = append(a, extra...)
	return len(a) + len(b)
}

func unknownConcat(a, b string, f func(string)) int {
	s := a + b
	f(s)
	return len(s)
}

func closureOnly(s string) int {
	read := func() int { return len(s) }
	return read() + len(s)
}

func localConcat(a, b string) int {
	s := a + b
	read := func() int { return len(s) }
	return read() + len(s)
}
`
	mod := compileOwnGoFree(t, src, true)
	if err := llvm.VerifyModule(mod, llvm.ReturnStatusAction); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"foo.staticConcat", "foo.staticAppend", "foo.staticConvert"} {
		body := mustNamedFunction(t, mod, name).String()
		if got := strings.Count(body, "runtime.FreeAccounted"); got == 0 {
			t.Errorf("%s runtime.FreeAccounted count = 0, want at least 1", name)
		}
		if got := strings.Count(body, "runtime.DecAccounted"); got != 0 {
			t.Errorf("%s runtime.DecAccounted count = %d, want 0", name, got)
		}
	}
	for _, name := range []string{"foo.aliasAppend", "foo.unknownConcat"} {
		body := mustNamedFunction(t, mod, name).String()
		if got := strings.Count(body, "runtime.FreeAccounted"); got != 0 {
			t.Errorf("%s runtime.FreeAccounted count = %d, want 0", name, got)
		}
		if got := strings.Count(body, "runtime.DecAccounted"); got != 0 {
			t.Errorf("%s runtime.DecAccounted count = %d, want 0", name, got)
		}
	}
	closure := mustNamedFunction(t, mod, "foo.closureOnly").String()
	local := mustNamedFunction(t, mod, "foo.localConcat").String()
	if got, base := strings.Count(local, "runtime.AllocCounted"), strings.Count(closure, "runtime.AllocCounted"); got <= base {
		t.Errorf("foo.localConcat runtime.AllocCounted count = %d, want more than foo.closureOnly %d", got, base)
	}
	if got, base := strings.Count(local, "runtime.DecAccounted"), strings.Count(closure, "runtime.DecAccounted"); got <= base {
		t.Errorf("foo.localConcat runtime.DecAccounted count = %d, want more than foo.closureOnly %d", got, base)
	}

	off := compileOwnGoFree(t, src, false)
	if got := strings.Count(mustNamedFunction(t, off, "foo.staticConcat").String(), "runtime.FreeAccounted"); got != 0 {
		t.Errorf("OwnGoFree false: foo.staticConcat runtime.FreeAccounted count = %d, want 0", got)
	}
}
