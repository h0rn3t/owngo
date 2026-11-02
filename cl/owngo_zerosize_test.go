package cl

import (
	"regexp"
	"strings"
	"testing"

	"github.com/xgo-dev/llvm"
)

// TestOwnGoZeroSizedNoFree: купова алокація нульового розміру — спільний
// символ модуля, а не блок купи (GOROOT/test append.go). Прохід не ставить
// на нього жодного входу володіння.
func TestOwnGoZeroSizedNoFree(t *testing.T) {
	const src = `package foo

type node struct{ next *node }

var sink func() int

func appendLiteral() int {
	s := append([]string{}, "x")
	return len(s)
}

func makeEmpty() int {
	s := append([]string{}, make([]string, 0)...)
	return len(s)
}

func emptyStruct() bool {
	p := &struct{}{}
	q := &struct{}{}
	return p == q
}

func closure() int {
	var e struct{}
	f := func() int { _ = e; return 1 }
	return f()
}
`
	mod := compileOwnGoFree(t, src, true)
	if err := llvm.VerifyModule(mod, llvm.ReturnStatusAction); err != nil {
		t.Fatal(err)
	}
	ir := mod.String()
	if !strings.Contains(ir, `@"__llgo.moduleZeroSizedAlloc$"`) {
		t.Fatal("fixture has no zero-sized heap allocation")
	}
	call := regexp.MustCompile(`call .*runtime\.(FreeAccounted|DecAccounted|Inc|DecCyclic|IncShared|DecShared)"\(ptr @"__llgo\.moduleZeroSizedAlloc\$"`)
	for _, line := range strings.Split(ir, "\n") {
		if call.MatchString(line) {
			t.Errorf("owngo op on the zero-sized sentinel: %s", strings.TrimSpace(line))
		}
	}
}
