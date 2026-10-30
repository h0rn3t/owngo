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

// AllocCounted виділяє malloc на size байтів користувача плюс слово
// лічильника, пише в це слово 1 і повертає вказівник за словом.
// Нульовий size дає спільний zerobase, як AllocU. Слово не атомарне
// і в розмір NoteAlloc не входить.
func AllocCounted(size uintptr) unsafe.Pointer {
	if size == 0 {
		return unsafe.Pointer(&zerobase)
	}
	word := unsafe.Sizeof(uintptr(0))
	block := AllocU(size + word)
	*(*uintptr)(block) = 1
	return unsafe.Add(block, word)
}

// Inc збільшує слово перед ptr на одиницю. zerobase і nil лишаються.
func Inc(ptr unsafe.Pointer) {
	if ptr == nil || ptr == unsafe.Pointer(&zerobase) {
		return
	}
	word := (*uintptr)(unsafe.Add(ptr, -int(unsafe.Sizeof(uintptr(0)))))
	*word++
}

// DecAccounted зменшує слово перед ptr на одиницю. На нулі звільняє адресу
// слова через FreeRoot і переносить size байтів користувача в лічильники
// FreeAccounted з категорією CatRCLocal. FreeAccounted на ptr не кличеться:
// це середина блока. zerobase нічого не звільняє. Слово не атомарне.
func DecAccounted(ptr unsafe.Pointer, size uintptr) {
	if ptr == nil || ptr == unsafe.Pointer(&zerobase) {
		return
	}
	word := (*uintptr)(unsafe.Add(ptr, -int(unsafe.Sizeof(uintptr(0)))))
	*word--
	if *word != 0 {
		return
	}
	FreeRoot(unsafe.Pointer(word))
	if size == 0 {
		return
	}
	atomic.Add(&freedBytes, uint64(size))
	atomic.Add(&liveBytes[CatRCLocal], -uint64(size))
}

// Freed повертає байти, які FreeAccounted і DecAccounted перенесли у звільнені.
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
