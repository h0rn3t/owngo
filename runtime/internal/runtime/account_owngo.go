//go:build owngo

package runtime

import (
	"unsafe"

	"github.com/xgo-dev/llgo/runtime/internal/sync/atomic"
)

// Номери збігаються з ownGoStatic … ownGoImplicit у cl.
const (
	CatStatic   int32 = 0
	CatRCLocal  int32 = 1
	CatRCShared int32 = 2
	CatCyclic   int32 = 3
	CatPinned   int32 = 4
	CatImplicit int32 = 5
	catCount    int32 = 6
)

var (
	freedBytes uint64
	liveBytes  [catCount]uint64
)

// NoteAlloc додає size байтів до живих місць категорії cat.
// Нульовий розмір і категорія поза 0…5 нічого не змінюють.
func NoteAlloc(size uintptr, cat int32) {
	if size == 0 || cat < 0 || cat >= catCount {
		return
	}
	atomic.Add(&liveBytes[cat], uint64(size))
}

// FreeAccounted звільняє блок malloc і переносить size байтів категорії cat
// із живих у звільнені. Нульовий розмір і категорія поза 0…5 блок звільняють,
// лічильники не чіпають. zerobase лишається.
func FreeAccounted(ptr unsafe.Pointer, size uintptr, cat int32) {
	FreeRoot(ptr)
	if size == 0 || cat < 0 || cat >= catCount {
		return
	}
	atomic.Add(&freedBytes, uint64(size))
	atomic.Add(&liveBytes[cat], -uint64(size))
}

// Freed повертає байти, які FreeAccounted переніс у звільнені.
func Freed() uint64 {
	return atomic.Load(&freedBytes)
}

// Live повертає байти категорії cat, які ще не звільнені.
// Категорія поза 0…5 дає 0.
func Live(cat int32) uint64 {
	if cat < 0 || cat >= catCount {
		return 0
	}
	return atomic.Load(&liveBytes[cat])
}

// ReportExit друкує байти, звільнені FreeAccounted, і байти, що дожили
// до виходу, за категорією місця.
func ReportExit() {
	freed := Freed()
	var live [catCount]uint64
	for cat := range live {
		live[cat] = Live(int32(cat))
	}
	println("owngo freed", freed)
	printed := false
	for cat, n := range live {
		if n == 0 {
			continue
		}
		printed = true
		var label string
		switch int32(cat) {
		case CatStatic:
			label = "static"
		case CatRCLocal:
			label = "rc-local"
		case CatRCShared:
			label = "rc-shared"
		case CatCyclic:
			label = "cyclic"
		case CatPinned:
			label = "pinned"
		case CatImplicit:
			label = "implicit"
		}
		println("owngo live", label, n)
	}
	if !printed {
		println("owngo live", 0)
	}
}
