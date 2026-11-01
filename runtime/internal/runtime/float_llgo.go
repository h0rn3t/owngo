// Copyright 2026 The XGo Authors. All rights reserved.
// Use of this source code is governed by the Apache 2.0 license.

package runtime

import (
	"unsafe"

	c "github.com/xgo-dev/llgo/runtime/internal/clite"
)

// The build reuses GOROOT runtime/float.go without these two bit casts:
// there they take the parameter's address, which LLGo's SSA builder puts on
// the heap. A stack slot reads the same bits.

func float64bits(f float64) uint64 {
	p := c.Alloca(unsafe.Sizeof(f))
	*(*float64)(p) = f
	return *(*uint64)(p)
}

func float64frombits(b uint64) float64 {
	p := c.Alloca(unsafe.Sizeof(b))
	*(*uint64)(p) = b
	return *(*float64)(p)
}
