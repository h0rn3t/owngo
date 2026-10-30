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

// CyclicDesc описує тип вузла для DecCyclic: байти користувача і поля-вказівники.
// Дескриптор дає компілятор, рантайм граф типів не будує.
type CyclicDesc struct {
	Size  uintptr
	Edges []CyclicEdge
}

// CyclicEdge — поле-вказівник вузла: зміщення від вказівника користувача
// і дескриптор цілі. Ненульова ціль має бути вказівником AllocCounted.
type CyclicEdge struct {
	Off  uintptr
	Elem *CyclicDesc
}

// DecCyclic зменшує слово перед ptr і до повернення виконує trial deletion
// з ptr як коренем спроби. Спроба зменшує слова дітей так, ніби ребра
// вузла знято, і йде лише в дитину, чиє слово стало нулем. Якщо слово
// кореня після спроби нульове, відвідані вузли звільняються через FreeRoot
// на адресу слова, а їхні байти користувача переносяться в підсумок
// FreeAccounted з CatCyclic. Інакше зменшення відновлюються. zerobase і nil
// нічого не звільняють. Слово не атомарне. Обхід пам'яті не виділяє,
// але рекурсивний: глибина дорівнює довжині ланцюга, що звільняється.
func DecCyclic(ptr unsafe.Pointer, desc *CyclicDesc) {
	if ptr == nil || ptr == unsafe.Pointer(&zerobase) {
		return
	}
	root := countWord(ptr)
	*root--
	trialDec(ptr, desc, ptr)
	if *root != 0 {
		trialInc(ptr, desc, ptr)
		return
	}
	// Список зібраних вузлів іде через їхні слова й закінчується коренем.
	*root = uintptr(ptr)
	size := collectTrial(ptr, desc, ptr)
	for p := unsafe.Pointer(*root); p != ptr; {
		next := unsafe.Pointer(*countWord(p))
		FreeRoot(unsafe.Pointer(countWord(p)))
		p = next
	}
	FreeRoot(unsafe.Pointer(root))
	if size == 0 {
		return
	}
	atomic.Add(&freedBytes, uint64(size))
	atomic.Add(&liveBytes[CatCyclic], -uint64(size))
}

func countWord(ptr unsafe.Pointer) *uintptr {
	return (*uintptr)(unsafe.Add(ptr, -int(unsafe.Sizeof(uintptr(0)))))
}

// child повертає ціль ребра e вузла ptr або nil, якщо ціль не рахується.
func child(ptr unsafe.Pointer, e CyclicEdge) unsafe.Pointer {
	c := *(*unsafe.Pointer)(unsafe.Add(ptr, e.Off))
	if c == unsafe.Pointer(&zerobase) {
		return nil
	}
	return c
}

// trialDec знімає ребра ptr і заходить у дитину, чиє слово стало нулем.
// Корінь уже відвіданий, тож у нього не заходить.
func trialDec(ptr unsafe.Pointer, desc *CyclicDesc, root unsafe.Pointer) {
	if desc == nil {
		return
	}
	for _, e := range desc.Edges {
		c := child(ptr, e)
		if c == nil {
			continue
		}
		w := countWord(c)
		*w--
		if *w == 0 && c != root {
			trialDec(c, e.Elem, root)
		}
	}
}

// trialInc дзеркалить trialDec: дитина з нульовим словом була відвідана,
// тож у неї заходить перше відновлене ребро.
func trialInc(ptr unsafe.Pointer, desc *CyclicDesc, root unsafe.Pointer) {
	if desc == nil {
		return
	}
	for _, e := range desc.Edges {
		c := child(ptr, e)
		if c == nil {
			continue
		}
		w := countWord(c)
		*w++
		if *w == 1 && c != root {
			trialInc(c, e.Elem, root)
		}
	}
}

// collectTrial додає вузли з нульовим словом до списку в слові кореня
// і повертає суму їхніх байтів користувача разом із ptr.
func collectTrial(ptr unsafe.Pointer, desc *CyclicDesc, root unsafe.Pointer) uintptr {
	if desc == nil {
		return 0
	}
	size := desc.Size
	for _, e := range desc.Edges {
		c := child(ptr, e)
		if c == nil || c == root || *countWord(c) != 0 {
			continue
		}
		rootWord := countWord(root)
		*countWord(c) = *rootWord
		*rootWord = uintptr(c)
		size += collectTrial(c, e.Elem, root)
	}
	return size
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
