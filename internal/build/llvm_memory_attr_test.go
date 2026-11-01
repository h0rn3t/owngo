package build

import (
	"strings"
	"testing"

	"github.com/xgo-dev/llgo/internal/crosscompile"
)

func TestDropMemoryEffectsForBundledClang(t *testing.T) {
	const source = `
define void @f(ptr %p) #0 {
  ret void
}
declare void @g() #0
attributes #0 = { nounwind memory(readwrite, argmem: none, target_mem: none) }
`
	for _, test := range []struct {
		name      string
		clangRoot string
		want      bool
	}{
		{name: "host clang keeps memory", want: true},
		{name: "bundled clang drops memory", clangRoot: "/esp-clang", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			mod := parseWasmAggregateIR(t, source)
			ctx := &context{crossCompile: crosscompile.Export{ClangRoot: test.clangRoot}}
			dropMemoryEffectsForBundledClang(ctx, mod)
			ir := mod.String()
			if got := strings.Contains(ir, "memory("); got != test.want {
				t.Fatalf("memory attribute present = %v, want %v:\n%s", got, test.want, ir)
			}
			if !strings.Contains(ir, "nounwind") {
				t.Fatalf("other attributes dropped:\n%s", ir)
			}
		})
	}
}
