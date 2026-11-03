//go:build owngo && !owngoasan && (linux || darwin)

package runtime

import (
	"unsafe"

	c "github.com/xgo-dev/llgo/runtime/internal/clite"
)

// Алокатор owngo — _wrap/owngo_alloc.c (P8.3). Під цим тегом він замінює
// malloc і free файлів z_nogc.go і z_defer_nogc.go.
const owngoAllocLLGoFiles = "; _wrap/owngo_alloc.c"

//go:linkname owngoMalloc C.owngo_malloc
func owngoMalloc(size uintptr) c.Pointer

//go:linkname owngoFree C.owngo_free
func owngoFree(ptr c.Pointer)

//go:linkname owngoBase C.owngo_base
func owngoBase(ptr c.Pointer) c.Pointer

// AllocU allocates uninitialized memory and returns a non-nil pointer or panics.
// Zero-byte requests return the shared zerobase without allocating.
func AllocU(size uintptr) unsafe.Pointer {
	if size == 0 {
		return unsafe.Pointer(&zerobase)
	}
	ret := owngoMalloc(size)
	if ret == nil {
		panic("out of memory")
	}
	recordMemProfileAlloc(size)
	return ret
}

// AllocZ allocates zero-initialized memory.
func AllocZ(size uintptr) unsafe.Pointer {
	ret := AllocU(size)
	c.Memset(ret, 0, size)
	return ret
}

func AllocRoot(size uintptr) unsafe.Pointer {
	if size == 0 {
		return unsafe.Pointer(&zerobase)
	}
	ret := owngoMalloc(size)
	if ret == nil {
		panic("out of memory")
	}
	return ret
}

// FreeRoot звільняє блок AllocRoot. nil і zerobase нічого не звільняють, як
// free(nil): рядок з порожнього зрізу має data == nil.
func FreeRoot(ptr unsafe.Pointer) {
	if ptr == nil || ptr == unsafe.Pointer(&zerobase) {
		return
	}
	owngoFree(ptr)
}

// FreeDeferNode releases a defer node allocated by AllocU.
func FreeDeferNode(ptr unsafe.Pointer) {
	if ptr != nil {
		owngoFree(ptr)
	}
}

// Base повертає початок блока, виділеного AllocU, AllocZ, AllocRoot або
// AllocCounted, для будь-якого вказівника всередині блока. Для блока
// AllocCounted це адреса слова лічильника. Для адреси поза купою —
// глобальної змінної, стека, zerobase — nil.
func Base(ptr unsafe.Pointer) unsafe.Pointer {
	return owngoBase(ptr)
}

// AddCleanupPtr is not implemented when GC is disabled.
// Cleanup functions will never be called.
func AddCleanupPtr(ptr unsafe.Pointer, cleanup func()) (cancel func()) {
	return func() {} // no-op cancel
}

func AddCancelableCleanupPtr(ptr unsafe.Pointer, cleanup func()) uint64 {
	return 0
}

func StopCleanupPtr(id uint64) {}
