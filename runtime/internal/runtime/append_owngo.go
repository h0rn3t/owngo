//go:build owngo

package runtime

import "github.com/xgo-dev/llgo/runtime/internal/runtime/math"

// OwnedAppendCap повертає ємність нового буфера append, який owngo виділяє
// сам (cl, emitOwnedAppend): як growslice, щоб cap збігався з llgo без тегу.
// Довжина поза діапазоном панікує тією самою помилкою.
func OwnedAppendCap(newLen, oldCap, etSize int) int {
	if newLen < 0 {
		panicgrowslicelen()
	}
	newCap := oldCap
	if newLen > oldCap {
		newCap = nextslicecap(newLen, oldCap)
	}
	mem, overflow := math.MulUintptr(uintptr(etSize), uintptr(newCap))
	if overflow || mem > maxAlloc {
		panicgrowslicelen()
	}
	return newCap
}
