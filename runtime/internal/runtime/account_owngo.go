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
// нічого не звільняють. Слово не атомарне.
//
// Обхід пам'яті не виділяє і стеком не росте. Слово, що стало нулем,
// жодне ребро вже не зменшує, тож воно тримає посилання: спершу в стеку
// роботи, потім у списку оброблених. Три нижні біти посилання — індекс
// дескриптора вузла в таблиці досяжних з кореня дескрипторів. Відкат іде
// списком оброблених від останнього: слово вузла знову стає лічильником
// раніше, ніж відновлюється ребро, яке довело його до нуля.
func DecCyclic(ptr unsafe.Pointer, desc *CyclicDesc) {
	if ptr == nil || ptr == unsafe.Pointer(&zerobase) {
		return
	}
	root := countWord(ptr)
	var table [cyclicTableLen]*CyclicDesc
	if !cyclicTable(desc, &table) {
		// Без обходу, як DecAccounted: діти лишають свої слова. Це витік, не
		// звільнення живого.
		*root--
		if *root != 0 {
			return
		}
		FreeRoot(unsafe.Pointer(root))
		if desc != nil {
			cyclicFreed(desc.Size)
		}
		return
	}
	*root--
	var work, done uintptr
	work = trialEdges(ptr, desc, ptr, &table, work)
	for work != 0 {
		node := unsafe.Pointer(work)
		w := countWord(node)
		work = *w &^ cyclicIndexMask
		index := *w & cyclicIndexMask
		*w = done | index
		done = uintptr(node)
		work = trialEdges(node, table[index], ptr, &table, work)
	}
	if *root != 0 {
		for done != 0 {
			node := unsafe.Pointer(done)
			w := countWord(node)
			done = *w &^ cyclicIndexMask
			index := *w & cyclicIndexMask
			*w = 0
			restoreEdges(node, table[index])
		}
		restoreEdges(ptr, desc)
		return
	}
	size := desc.Size
	for done != 0 {
		node := unsafe.Pointer(done)
		w := countWord(node)
		done = *w &^ cyclicIndexMask
		size += table[*w&cyclicIndexMask].Size
		FreeRoot(unsafe.Pointer(w))
	}
	FreeRoot(unsafe.Pointer(root))
	cyclicFreed(size)
}

// Таблиця досяжних дескрипторів: індекс уміщається в три нижні біти
// вказівника вузла, бо вказівник AllocCounted вирівняний на слово.
const (
	cyclicTableLen  = 8
	cyclicIndexMask = cyclicTableLen - 1
)

// cyclicTable збирає дескриптори, досяжні з desc за ребрами, з desc під
// індексом 0. false — дескриптора немає, ребро без цілі або їх понад вісім.
func cyclicTable(desc *CyclicDesc, table *[cyclicTableLen]*CyclicDesc) bool {
	if desc == nil {
		return false
	}
	table[0] = desc
	n := 1
	for i := 0; i < n; i++ {
		for _, e := range table[i].Edges {
			if e.Elem == nil {
				return false
			}
			if cyclicIndex(table, e.Elem) >= 0 {
				continue
			}
			if n == cyclicTableLen {
				return false
			}
			table[n] = e.Elem
			n++
		}
	}
	return true
}

func cyclicIndex(table *[cyclicTableLen]*CyclicDesc, desc *CyclicDesc) int {
	for i, d := range table {
		if d == desc {
			return i
		}
	}
	return -1
}

// trialEdges знімає ребра node і кладе в стек роботи дитину, чиє слово
// стало нулем. Корінь у стек не йде: його ребра вже зняті.
func trialEdges(node unsafe.Pointer, desc *CyclicDesc, root unsafe.Pointer, table *[cyclicTableLen]*CyclicDesc, work uintptr) uintptr {
	for _, e := range desc.Edges {
		c := child(node, e)
		if c == nil {
			continue
		}
		w := countWord(c)
		*w--
		if *w == 0 && c != root {
			*w = work | uintptr(cyclicIndex(table, e.Elem))
			work = uintptr(c)
		}
	}
	return work
}

func restoreEdges(node unsafe.Pointer, desc *CyclicDesc) {
	for _, e := range desc.Edges {
		if c := child(node, e); c != nil {
			*countWord(c)++
		}
	}
}

func cyclicFreed(size uintptr) {
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
