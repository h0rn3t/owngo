package census

import (
	"testing"

	"golang.org/x/tools/go/ssa"
)

// TestRuntimeBorrow — P9.7: функції stdlib, які рантайм llgo реалізує
// і які адресу лише позичають, і підказка abi.Escape не роблять
// параметр викликача невідомим чи глобальною втечею.
func TestRuntimeBorrow(t *testing.T) {
	tests := []struct {
		path, src, fn string
		unknown       bool
		side          uint8
	}{
		{
			path: "sync/atomic",
			src: `package p

func AddUint64(addr *uint64, delta uint64) uint64

func bump(p *uint64) { AddUint64(p, 1) }
`,
			fn: "bump",
		},
		{
			path: "sync/atomic",
			src: `package p

func Mystery(addr *uint64)

func bump(p *uint64) { Mystery(p) }
`,
			fn:      "bump",
			unknown: true,
		},
		{
			path: "internal/abi",
			src: `package p

var (
	alwaysFalse bool
	escapeSink  any
)

func Escape(x *int) *int {
	if alwaysFalse {
		escapeSink = x
	}
	return x
}

func hint(p *int) { _ = Escape(p) }
`,
			fn: "hint",
		},
		{
			path: "example.com/abi",
			src: `package p

var (
	alwaysFalse bool
	escapeSink  any
)

func Escape(x *int) *int {
	if alwaysFalse {
		escapeSink = x
	}
	return x
}

func hint(p *int) { _ = Escape(p) }
`,
			fn:   "hint",
			side: uint8(escapeGlobal),
		},
	}
	for _, tt := range tests {
		pkg := buildOnePackage(t, tt.path, tt.src, ssa.SanityCheckFunctions)
		sums := packageSummaries(nil, ssaFuncs(pkg))
		got := sums[pkg.Func(tt.fn)].Params[0]
		if got.Unknown != tt.unknown || got.Side != tt.side {
			t.Errorf("%s.%s param = %+v, want Unknown %v, Side %d", tt.path, tt.fn, got, tt.unknown, tt.side)
		}
	}
}
