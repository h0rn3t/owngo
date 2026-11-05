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
	// CatRegion — байти регіонів (region_owngo.go). Компілятор цієї
	// категорії в NoteAlloc не передає: облік веде сам регіон.
	CatRegion   int32 = 6
	accountCats int32 = 7
)

// accountShard — лічильники обліку потоків, яким дістався цей шард. Живі
// байти шарду можуть піти в мінус, якщо місце звільняє інший потік:
// за модулем 2⁶⁴ сума шардів точна. Шард займає 128 байтів: лічильники
// сусідніх шардів розділяють понад 64 байти, тож за будь-якого вирівнювання
// масиву вони не ділять кеш-лінію.
type accountShard struct {
	freed uint64
	live  [accountCats]uint64
	_     [128 - 8 - 8*accountCats]byte
}

// Потоків понад accountShardCount ділять шарди по колу, тому додавання
// лишаються атомарними; без спільного шарду кеш-лінія не переходить між ядрами.
const accountShardCount = 64

var (
	accountShards    [accountShardCount]accountShard
	nextAccountShard uint32
)

// currentAccountShard — номер шарду потоку плюс один; 0 — шард ще не
// призначено. Номер, а не вказівник: змінна TLS із вказівником іде в блок
// пакета через LocalPackage, і NoteAlloc на одному ядрі повільніший на 9%.
//
//llgointernal:tls
var currentAccountShard uint32

// myShardIndex — номер шарду потоку, спільний для обліку і лічильників операцій.
func myShardIndex() uint32 {
	i := currentAccountShard
	if i == 0 {
		i = atomic.Add(&nextAccountShard, 1)%accountShardCount + 1
		currentAccountShard = i
	}
	return (i - 1) % accountShardCount
}

func myAccountShard() *accountShard {
	return &accountShards[myShardIndex()]
}

// Операції, які рахує тег owngostat (countOp). Номер opNoteAlloc+cat —
// виклик NoteAlloc категорії cat. Рахується кожен виклик, і з nil чи zerobase.
const (
	opAlloc int32 = iota
	opAllocRoot
	opAllocCounted
	opInc
	opDecAccounted
	opDecCyclic
	opCyclicVisit // вузол, крім кореня, відвіданий trial deletion
	opIncShared
	opDecShared
	opFreeAccounted
	opFreeRoot
	opNoteAlloc
)

// Операції регіону P-NEW-05.4 — після рядків NoteAlloc, щоб номери
// попередніх не зсунулись.
const (
	opRegionCreate = opNoteAlloc + catCount + iota
	opAllocRegion
	opFreeRegion
	opCount
)

// NoteAlloc додає size байтів до живих місць категорії cat.
// Нульовий розмір і категорія поза 0…5 нічого не змінюють.
func NoteAlloc(size uintptr, cat int32) {
	if cat < 0 || cat >= catCount {
		return
	}
	countOp(opNoteAlloc + cat)
	if size == 0 {
		return
	}
	atomic.Add(&myAccountShard().live[cat], uint64(size))
}

// noteFree переносить size байтів категорії cat із живих у звільнені.
// Нульовий розмір і категорія поза 0…5 нічого не змінюють.
func noteFree(size uintptr, cat int32) {
	if size == 0 || cat < 0 || cat >= catCount {
		return
	}
	// Живі зменшуються раніше: хто бачить байти у Freed, бачить їх і поза Live.
	s := myAccountShard()
	atomic.Add(&s.live[cat], -uint64(size))
	atomic.Add(&s.freed, uint64(size))
}

// FreeAccounted звільняє блок malloc і переносить size байтів категорії cat
// із живих у звільнені. Нульовий розмір і категорія поза 0…5 блок звільняють,
// лічильники не чіпають. zerobase лишається.
func FreeAccounted(ptr unsafe.Pointer, size uintptr, cat int32) {
	countOp(opFreeAccounted)
	if !isZeroBase(ptr) {
		FreeRoot(ptr)
	}
	noteFree(size, cat)
}

// AllocCounted виділяє malloc на size байтів користувача плюс слово
// лічильника, пише в це слово 1 і повертає вказівник за словом.
// Нульовий size дає спільний zerobase, як AllocU. Слово не атомарне
// і в розмір NoteAlloc не входить.
func AllocCounted(size uintptr) unsafe.Pointer {
	countOp(opAllocCounted)
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
	countOp(opInc)
	if ptr == nil || isZeroBase(ptr) {
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
	countOp(opDecAccounted)
	if ptr == nil || isZeroBase(ptr) {
		return
	}
	word := (*uintptr)(unsafe.Add(ptr, -int(unsafe.Sizeof(uintptr(0)))))
	*word--
	if *word != 0 {
		return
	}
	FreeRoot(unsafe.Pointer(word))
	noteFree(size, CatRCLocal)
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
	countOp(opDecCyclic)
	if ptr == nil || isZeroBase(ptr) {
		return
	}
	root := countWord(ptr)
	table, ok := cyclicTable(desc)
	if !ok {
		// Без обходу, як DecAccounted: діти лишають свої слова. Це витік, не
		// звільнення живого.
		*root--
		if *root != 0 {
			return
		}
		FreeRoot(unsafe.Pointer(root))
		if desc != nil {
			noteFree(desc.Size, CatCyclic)
		}
		return
	}
	*root--
	var work, done uintptr
	work = trialEdges(ptr, desc, ptr, table, work)
	for work != 0 {
		countOp(opCyclicVisit)
		node := unsafe.Pointer(work)
		w := countWord(node)
		work = *w &^ cyclicIndexMask
		index := *w & cyclicIndexMask
		*w = done | index
		done = uintptr(node)
		work = trialEdges(node, table[index], ptr, table, work)
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
	noteFree(size, CatCyclic)
}

// Таблиця досяжних дескрипторів: індекс уміщається в три нижні біти
// вказівника вузла, бо вказівник AllocCounted вирівняний на слово.
const (
	cyclicTableLen  = 8
	cyclicIndexMask = cyclicTableLen - 1
)

// cyclicTable збирає дескриптори, досяжні з desc за ребрами, з desc під
// індексом 0. false — дескриптора немає, ребро без цілі або їх понад вісім.
// Таблиця йде за значенням: адреса локального масиву перенесла б його
// в купу, і прохід звільняв би його як static на кожному виклику.
func cyclicTable(desc *CyclicDesc) (table [cyclicTableLen]*CyclicDesc, ok bool) {
	if desc == nil {
		return table, false
	}
	table[0] = desc
	n := 1
	for i := 0; i < n; i++ {
		for _, e := range table[i].Edges {
			if e.Elem == nil {
				return table, false
			}
			if cyclicIndex(table, e.Elem) >= 0 {
				continue
			}
			if n == cyclicTableLen {
				return table, false
			}
			table[n] = e.Elem
			n++
		}
	}
	return table, true
}

func cyclicIndex(table [cyclicTableLen]*CyclicDesc, desc *CyclicDesc) int {
	for i, d := range table {
		if d == desc {
			return i
		}
	}
	return -1
}

// trialEdges знімає ребра node і кладе в стек роботи дитину, чиє слово
// стало нулем. Корінь у стек не йде: його ребра вже зняті.
func trialEdges(node unsafe.Pointer, desc *CyclicDesc, root unsafe.Pointer, table [cyclicTableLen]*CyclicDesc, work uintptr) uintptr {
	for _, e := range desc.Edges {
		c := child(node, e.Off)
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
		if c := child(node, e.Off); c != nil {
			*countWord(c)++
		}
	}
}

// SharedDesc описує тип об'єкта спільної втечі для DecShared: байти
// користувача, категорію підсумку і поля, які проходить каскад.
type SharedDesc struct {
	Size  uintptr
	Cat   int32
	Edges []SharedEdge
}

// SharedEdge — поле об'єкта спільної втечі зі зміщенням Off від вказівника
// користувача. Ненульова ціль — вказівник AllocCounted. Shared задає дитину
// спільної втечі: вона отримує DecShared. Інакше Cyclic задає переміщену
// дитину cyclic: DecCyclic. Без обох дитина отримує DecAccounted з Size.
type SharedEdge struct {
	Off    uintptr
	Shared *SharedDesc
	Cyclic *CyclicDesc
	Size   uintptr
}

// IncShared атомарно збільшує слово перед ptr на одиницю. zerobase і nil
// лишаються.
func IncShared(ptr unsafe.Pointer) {
	countOp(opIncShared)
	if ptr == nil || isZeroBase(ptr) {
		return
	}
	atomic.Add(countWord(ptr), 1)
}

// DecShared атомарно зменшує слово перед ptr на одиницю. Зменшення
// послідовно узгоджене, тож воно має порядок release, а на нулі — acquire:
// усе, що інші горутини робили з об'єктом до своїх DecShared, видно тій,
// що звільняє. На нулі об'єкт і все, що досяжне лише через нього, бачить
// одна горутина: каскад проходить поля за desc, звільняє вузли через
// FreeRoot на адресу слова і переносить їхні байти користувача в підсумок
// FreeAccounted з категорією дескриптора вузла. zerobase і nil нічого не
// звільняють.
//
// Пробного видалення немає: цикл через спільний об'єкт лишається живим.
// Обхід пам'яті не виділяє і стеком не росте: слово вузла, що став нулем,
// тримає посилання стеку роботи, а три нижні біти — індекс дескриптора
// в таблиці досяжних з desc дескрипторів.
func DecShared(ptr unsafe.Pointer, desc *SharedDesc) {
	countOp(opDecShared)
	if ptr == nil || isZeroBase(ptr) {
		return
	}
	root := countWord(ptr)
	if atomic.Sub(root, 1) != 1 {
		return
	}
	table, ok := sharedTable(desc)
	if !ok {
		// Без каскаду: діти лишають свої слова. Це витік, не звільнення живого.
		FreeRoot(unsafe.Pointer(root))
		if desc != nil {
			noteFree(desc.Size, desc.Cat)
		}
		return
	}
	var sizes [catCount]uintptr
	*root = 0
	work := uintptr(ptr)
	for work != 0 {
		node := unsafe.Pointer(work)
		w := countWord(node)
		work = *w &^ cyclicIndexMask
		d := table[*w&cyclicIndexMask]
		for _, e := range d.Edges {
			c := child(node, e.Off)
			switch {
			case c == nil:
			case e.Shared != nil:
				if cw := countWord(c); atomic.Sub(cw, 1) == 1 {
					*cw = work | uintptr(sharedIndex(table, e.Shared))
					work = uintptr(c)
				}
			case e.Cyclic != nil:
				DecCyclic(c, e.Cyclic)
			default:
				DecAccounted(c, e.Size)
			}
		}
		FreeRoot(unsafe.Pointer(w))
		if d.Cat >= 0 && d.Cat < catCount {
			sizes[d.Cat] += d.Size
		}
	}
	for cat, size := range sizes {
		noteFree(size, int32(cat))
	}
}

// sharedTable збирає дескриптори, досяжні з desc за ребрами Shared, з desc
// під індексом 0. false — дескриптора немає або їх понад вісім. Таблиця
// йде за значенням: адреса локального масиву перенесла б його в купу.
func sharedTable(desc *SharedDesc) (table [cyclicTableLen]*SharedDesc, ok bool) {
	if desc == nil {
		return table, false
	}
	table[0] = desc
	n := 1
	for i := 0; i < n; i++ {
		for _, e := range table[i].Edges {
			if e.Shared == nil || sharedIndex(table, e.Shared) >= 0 {
				continue
			}
			if n == cyclicTableLen {
				return table, false
			}
			table[n] = e.Shared
			n++
		}
	}
	return table, true
}

func sharedIndex(table [cyclicTableLen]*SharedDesc, desc *SharedDesc) int {
	for i, d := range table {
		if d == desc {
			return i
		}
	}
	return -1
}

// moduleZero — спільний символ купових алокацій нульового розміру, який
// кодоген кладе в модулі (ssa moduleZeroSizedAlloc); лінкер лишає одну копію.
// Функція іншого модуля повертає його як блок, наприклад []byte("") зі
// StringToBytes.
//
//go:linkname moduleZero C.__llgo.moduleZeroSizedAlloc$
var moduleZero byte

// isZeroBase повідомляє, чи ptr — спільна база нульового розміру, а не блок malloc.
func isZeroBase(ptr unsafe.Pointer) bool {
	return ptr == unsafe.Pointer(&zerobase) || ptr == unsafe.Pointer(&moduleZero)
}

func countWord(ptr unsafe.Pointer) *uintptr {
	return (*uintptr)(unsafe.Add(ptr, -int(unsafe.Sizeof(uintptr(0)))))
}

// child повертає ціль поля зі зміщенням off вузла ptr або nil, якщо ціль
// не рахується.
func child(ptr unsafe.Pointer, off uintptr) unsafe.Pointer {
	c := *(*unsafe.Pointer)(unsafe.Add(ptr, off))
	if isZeroBase(c) {
		return nil
	}
	return c
}

// Freed повертає байти, які FreeAccounted і DecAccounted перенесли у звільнені.
func Freed() uint64 {
	var n uint64
	for i := range accountShards {
		n += atomic.Load(&accountShards[i].freed)
	}
	return n
}

// Live повертає байти категорії cat, які ще не звільнені.
// Категорія поза 0…6 дає 0.
func Live(cat int32) uint64 {
	if cat < 0 || cat >= accountCats {
		return 0
	}
	var n uint64
	for i := range accountShards {
		n += atomic.Load(&accountShards[i].live[cat])
	}
	return n
}

// ReportExit друкує байти, звільнені FreeAccounted, і байти, що дожили
// до виходу, за категорією місця. З тегом owngostat далі йдуть рядки
// owngo ops з кількістю кожної операції.
func ReportExit() {
	freed := Freed()
	var live [accountCats]uint64
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
		case CatRegion:
			label = "region"
		}
		println("owngo live", label, n)
	}
	if !printed {
		println("owngo live", 0)
	}
	reportOps()
}
