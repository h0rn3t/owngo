package cl

import (
	"strings"
	"testing"

	"github.com/xgo-dev/llvm"
)

// TestOwnGoShared — P9.3: змінна, захоплена замиканням go, отримує
// AllocCounted, IncShared перед go, DecShared у функції і DecShared
// у горутині після повернення її функції. Запис NewTree у захоплену
// змінну — переміщення без Inc. Місце, що ще йде в глобальну змінну,
// DecShared не отримує.
func TestOwnGoShared(t *testing.T) {
	const src = `package foo

type Tree struct{ Left, Right *Tree }

func NewTree(depth int) *Tree {
	if depth > 0 {
		return &Tree{Left: NewTree(depth - 1), Right: NewTree(depth - 1)}
	}
	return &Tree{}
}

func (t *Tree) Count() int {
	if t.Left == nil {
		return 1
	}
	return 1 + t.Left.Count() + t.Right.Count()
}

type Counter struct{ n int }

func (c *Counter) Add(d int) { c.n += d }

var sink *Counter

func capture() int {
	var c Counter
	go func() { c.Add(1) }()
	c.Add(2)
	return c.n
}

func tree() int {
	var t *Tree
	go func() { t = NewTree(3) }()
	return t.Count()
}

func global() {
	var c Counter
	sink = &c
	go func() { c.Add(1) }()
}
`
	mod := compileOwnGoFree(t, src, true)
	if err := llvm.VerifyModule(mod, llvm.ReturnStatusAction); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"foo.capture", "foo.tree"} {
		body := mustNamedFunction(t, mod, name).String()
		alloc := strings.Index(body, "runtime.AllocCounted")
		inc := strings.Index(body, "runtime.IncShared")
		proc := strings.Index(body, "runtime.NewProc")
		dec := strings.LastIndex(body, "runtime.DecShared")
		if alloc < 0 || inc < 0 || proc < 0 || dec < 0 {
			t.Errorf("%s AllocCounted at %d, IncShared at %d, NewProc at %d, DecShared at %d, want all\n%s", name, alloc, inc, proc, dec, body)
			continue
		}
		if alloc > inc || inc > proc || proc > dec {
			t.Errorf("%s order AllocCounted %d, IncShared %d, NewProc %d, DecShared %d, want increasing\n%s", name, alloc, inc, proc, dec, body)
		}
	}
	routines, freed := 0, 0
	for fn := mod.FirstFunction(); !fn.IsNil(); fn = llvm.NextFunction(fn) {
		if !strings.HasPrefix(fn.Name(), "foo._llgo_routine$") {
			continue
		}
		if strings.Contains(fn.String(), "runtime.DecShared") {
			routines++
		}
		if strings.Contains(fn.String(), "runtime.FreeAccounted") {
			freed++
		}
	}
	if routines != 2 {
		t.Errorf("goroutine routines with DecShared = %d, want 2 (capture, tree)", routines)
	}
	// P9.7: замикання, яке вживає лише go, рутина звільняє після виклику.
	if freed != 3 {
		t.Errorf("goroutine routines freeing the closure context = %d, want 3", freed)
	}
	body := mustNamedFunction(t, mod, "foo.tree$1").String()
	if strings.Contains(body, "runtime.Inc") {
		t.Errorf("foo.tree$1 has Inc, want the fresh tree moved into the cell\n%s", body)
	}
	body = mustNamedFunction(t, mod, "foo.global").String()
	for _, call := range []string{"runtime.IncShared", "runtime.DecShared", "runtime.AllocCounted"} {
		if strings.Contains(body, call) {
			t.Errorf("foo.global has %s, want none: the variable also escapes to a global\n%s", call, body)
		}
	}
}
