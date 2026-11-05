//go:build !llgo

package math

import "unsafe"

// c.Alloca is an LLGo intrinsic; host go test builds take the plain casts.
func fmaFloat64bits(x float64) uint64     { return *(*uint64)(unsafe.Pointer(&x)) }
func fmaFloat64frombits(x uint64) float64 { return *(*float64)(unsafe.Pointer(&x)) }
