package cl

import (
	"strings"
	"testing"
)

// TestOwnGoPinned — P10.3: місце, яке стає unsafe.Pointer у викликаного чи
// у викликача (свіжий результат), закріплений вузол кільця і те, що лежить
// у його полі, не отримують ні FreeAccounted, ні Dec. Місце, яке викликаний
// зберігає як unsafe.Pointer чи uintptr, обліковується в pinned (категорія 4).
func TestOwnGoPinned(t *testing.T) {
	const src = `package foo

import "unsafe"

type T struct{ a, b int }

type Node struct {
	next *Node
	data *T
}

var (
	addr uintptr
	keep unsafe.Pointer
)

func New() *T { return &T{} }

func callerPins() {
	p := New()
	addr = uintptr(unsafe.Pointer(p))
}

func stash(p *T) { addr = uintptr(unsafe.Pointer(p)) }

func calleePins() int {
	p := &T{}
	stash(p)
	return p.a
}

func hold(p *T) { keep = unsafe.Pointer(p) }

func calleeKeeps() int {
	p := &T{}
	hold(p)
	return p.a
}

func fieldPins() int {
	a := &Node{}
	b := &Node{}
	a.next = b
	b.next = a
	t := &T{}
	b.data = t
	keep = unsafe.Pointer(b)
	return t.a
}
`
	mod := compileOwnGoFree(t, src, true)
	frees := []string{"runtime.FreeAccounted", "runtime.DecAccounted", "runtime.DecCyclic", "runtime.DecShared"}
	tests := []struct {
		fn     string
		pinned int // NoteAlloc з категорією pinned
	}{
		// Свіжий результат облікує New у своїй категорії.
		{fn: "callerPins", pinned: 0},
		{fn: "calleePins", pinned: 1},
		{fn: "calleeKeeps", pinned: 1},
		{fn: "fieldPins", pinned: 1},
	}
	for _, tt := range tests {
		t.Run(tt.fn, func(t *testing.T) {
			ir := mustNamedFunction(t, mod, "foo."+tt.fn).String()
			for _, name := range frees {
				if strings.Contains(ir, name) {
					t.Errorf("foo.%s IR contains %s, want no free of a pinned site", tt.fn, name)
				}
			}
			pinned := 0
			for line := range strings.Lines(ir) {
				if strings.Contains(line, "runtime.NoteAlloc") && strings.HasSuffix(strings.TrimSpace(line), ", i32 4)") {
					pinned++
				}
			}
			if pinned != tt.pinned {
				t.Errorf("foo.%s NoteAlloc pinned = %d, want %d", tt.fn, pinned, tt.pinned)
			}
		})
	}
}

// TestOwnGoPinnedWarning — P10.2: місце pinned пакета, якому збірка дала
// OwnGoWarn, дає один рядок попередження з позицією місця. Пакет без unsafe
// і збірка без тегу owngo рядків не дають.
func TestOwnGoPinnedWarning(t *testing.T) {
	const pinned = `package foo

import "unsafe"

type T struct{ a, b int }

var keep unsafe.Pointer

func Pin() {
	keep = unsafe.Pointer(&T{})
}
`
	const plain = `package foo

type T struct{ a, b int }

var keep *T

func Keep() {
	keep = &T{}
}
`
	// Позиція місця — Lbrace літерала, як у діагностиках census.
	const line = "foo.go:10:26: owngo: pinned: allocation converted to unsafe.Pointer stays live until exit\n"
	tests := []struct {
		name string
		src  string
		free bool
		want string
	}{
		{name: "pinned", src: pinned, free: true, want: line},
		{name: "plain", src: plain, free: true, want: ""},
		{name: "notag", src: pinned, free: false, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ssaPkg, _, files := buildGoSSAPkg(t, tt.src)
			prog := newLLSSAProg(t)
			t.Cleanup(prog.Dispose)
			var warn strings.Builder
			_, _, err := NewPackageExWithEmbedMetaOptions(
				prog, nil, nil, nil, ssaPkg, files, nil, false,
				Options{OwnGoFree: tt.free, OwnGoWarn: &warn},
			)
			if err != nil {
				t.Fatal(err)
			}
			if got := warn.String(); got != tt.want {
				t.Errorf("OwnGoWarn output = %q, want %q", got, tt.want)
			}
		})
	}
}
