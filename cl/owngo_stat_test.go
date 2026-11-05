package cl

import (
	"regexp"
	"strings"
	"testing"

	"github.com/xgo-dev/llvm"
)

// ownGoOpCall — виклик операції рантайму, яку codegen ставить місцю.
var ownGoOpCall = regexp.MustCompile(`call [^\n]*@"[^"]*runtime\.(AllocZ|AllocU|AllocCounted|NoteAlloc|FreeAccounted|DecAccounted|DecCyclic|Inc)"\(`)

// TestOwnGoStatSites — P-NEW-02.1: під OwnGoStat кожна операція рантайму
// місця static, rc-local і cyclic отримує свій лічильник, а таблиця пакета
// називає позицію, категорію, причину й операцію. Без OwnGoStat ні
// лічильників, ні таблиці немає.
func TestOwnGoStatSites(t *testing.T) {
	const src = `package foo

type Tree struct {
	Left, Right *Tree
}

func NewTree(depth int) *Tree {
	if depth > 0 {
		return &Tree{Left: NewTree(depth - 1), Right: NewTree(depth - 1)}
	}
	return &Tree{}
}

func local(depth int) int {
	a := NewTree(depth)
	if a.Left == nil {
		return 1
	}
	return 2
}

func staticNew() int {
	p := new(int)
	*p = 1
	return *p
}

func closureUse() int {
	p := new(int)
	*p = 1
	f := func() int { return *p }
	return f() + *p
}
`
	mod := compileOwnGo(t, src, Options{OwnGoFree: true, OwnGoStat: true})
	if err := llvm.VerifyModule(mod, llvm.ReturnStatusAction); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"foo.NewTree", "foo.local", "foo.staticNew", "foo.closureUse"} {
		body := mustNamedFunction(t, mod, name).String()
		ops := len(ownGoOpCall.FindAllString(body, -1))
		counters := strings.Count(body, `atomicrmw add ptr @"foo.__owngo_site$`)
		if ops == 0 || counters != ops {
			t.Errorf("%s: %d site counters for %d runtime ops, want one per op", name, counters, ops)
		}
	}
	ir := mod.String()
	for _, row := range []string{
		`:23 static - alloc`,
		`:23 static - note_alloc`,
		`:23 static - free_accounted`,
		`:29 rc-local [^ "]+ alloc_counted`,
		`:29 rc-local [^ "]+ dec_accounted`,
		`:9 cyclic [^ "]+ alloc_counted`,
		`:15 cyclic - dec_cyclic`,
	} {
		if !regexp.MustCompile(row + `"`).MatchString(ir) {
			t.Errorf("site table has no row matching %q", row)
		}
	}
	// Конструктор модуля, а не init: пакет LLGoPackage = "link" init не має.
	if !strings.Contains(mustNamedFunction(t, mod, "foo.__owngo_sites$ctor").String(), "runtime.RegisterSites") {
		t.Error("foo.__owngo_sites$ctor has no runtime.RegisterSites call")
	}
	if !regexp.MustCompile(`__owngo_sites\$ctor.*section "\.init_array"|@llvm\.global_ctors = appending .*__owngo_sites\$ctor`).MatchString(ir) {
		t.Error("module does not run foo.__owngo_sites$ctor as a constructor")
	}

	off := compileOwnGo(t, src, Options{OwnGoFree: true}).String()
	for _, s := range []string{"__owngo_site", "RegisterSites", "atomicrmw"} {
		if strings.Contains(off, s) {
			t.Errorf("OwnGoStat false: module has %q, want no site counters", s)
		}
	}
}

func compileOwnGo(t *testing.T, src string, opts Options) llvm.Module {
	t.Helper()
	ssaPkg, _, files := buildGoSSAPkg(t, src)
	prog := newLLSSAProg(t)
	t.Cleanup(prog.Dispose)
	pkg, _, err := NewPackageExWithEmbedMetaOptions(prog, nil, nil, nil, ssaPkg, files, nil, false, opts)
	if err != nil {
		t.Fatal(err)
	}
	return pkg.Module()
}
