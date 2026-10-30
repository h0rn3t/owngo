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
