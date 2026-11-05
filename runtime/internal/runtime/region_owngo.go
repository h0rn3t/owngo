//go:build owngo

package runtime

import (
	"unsafe"

	c "github.com/xgo-dev/llgo/runtime/internal/clite"
	"github.com/xgo-dev/llgo/runtime/internal/sync/atomic"
)

// Регіон (P-NEW-05.3): пам'ять структури з одним коренем, яку компілятор
// звільняє цілою в точці смерті кореня. Регіон належить одній горутині,
// тож синхронізації немає.
//
// Об'єкти лягають послідовним зсувом у шматки по regionChunk байтів.
// Шматок — блок AllocRoot: розмір входить у малий клас алокатора P8.3, тож
// шматки бере і повертає купа потоку без блокування, а покинуту купу
// забирає наступний потік разом із вільними блоками. Це і є кеш шматків
// потоку: окремий кеш у TLS загубив би шматки, коли горутина завершується.
//
// Об'єкт, більший за половину шматка, отримує окремий блок у списку
// регіону. Під owngoasan окремий блок отримує кожен об'єкт
// (regionPerObject), щоб ASan бачив читання після FreeRegion.
const (
	regionChunk = 8192
	regionAlign = 16
	// regionLink — заголовок шматка і окремого блока: посилання на
	// наступний, з відступом до вирівнювання об'єктів.
	regionLink = regionAlign
)

type region struct {
	cur, end unsafe.Pointer // вільне місце поточного шматка
	chunks   unsafe.Pointer // шматки, перший — той, у якому лежить region
	blocks   unsafe.Pointer // окремі блоки
	bytes    uintptr        // байти користувача всіх алокацій
	noted    uintptr        // з них уже додані до Live(CatRegion)
	// profile — об'єкти за кошиком профілю пам'яті, ще не додані до нього.
	// Шматок вміщає до regionChunk/regionAlign об'єктів, тож uint32 досить.
	profile [len(memProfileBuckets)]uint32
}

const regionHeader = (unsafe.Sizeof(region{}) + regionAlign - 1) &^ (regionAlign - 1)

// RegionCreate повертає новий порожній регіон.
func RegionCreate() unsafe.Pointer {
	countOp(opRegionCreate)
	if regionPerObject {
		r := (*region)(AllocRoot(unsafe.Sizeof(region{})))
		*r = region{}
		return unsafe.Pointer(r)
	}
	chunk := AllocRoot(regionChunk)
	*(*unsafe.Pointer)(chunk) = nil
	r := (*region)(unsafe.Add(chunk, regionLink))
	*r = region{
		cur:    unsafe.Add(chunk, regionLink+regionHeader),
		end:    unsafe.Add(chunk, regionChunk),
		chunks: chunk,
	}
	return unsafe.Pointer(r)
}

// RegionAlloc повертає size обнулених байтів регіону r, вирівняних на 16.
// Нульовий size дає спільний zerobase. Слова лічильника, NoteAlloc і
// атомарних операцій на цьому шляху немає: Live(CatRegion) і профіль
// пам'яті отримують байти й об'єкти, коли регіон бере новий шматок чи
// блок, і в FreeRegion.
func RegionAlloc(rp unsafe.Pointer, size uintptr) unsafe.Pointer {
	countOp(opAllocRegion)
	if size == 0 {
		return unsafe.Pointer(&zerobase)
	}
	r := (*region)(rp)
	r.profile[memProfileBucket(size)]++
	r.bytes += size
	n := (size + regionAlign - 1) &^ (regionAlign - 1)
	if n <= uintptr(r.end)-uintptr(r.cur) {
		p := r.cur
		r.cur = unsafe.Add(p, n)
		c.Memset(p, 0, size)
		return p
	}
	return r.allocSlow(size, n)
}

// allocSlow бере окремий блок для великого об'єкта чи новий шматок і
// переносить накопичені байти в Live(CatRegion).
func (r *region) allocSlow(size, n uintptr) unsafe.Pointer {
	atomic.Add(&myAccountShard().live[CatRegion], uint64(r.bytes-r.noted))
	r.noted = r.bytes
	r.flushProfile()
	if regionPerObject || n > regionChunk/2 {
		block := AllocRoot(regionLink + n)
		*(*unsafe.Pointer)(block) = r.blocks
		r.blocks = block
		p := unsafe.Add(block, regionLink)
		c.Memset(p, 0, size)
		return p
	}
	chunk := AllocRoot(regionChunk)
	*(*unsafe.Pointer)(chunk) = r.chunks
	r.chunks = chunk
	p := unsafe.Add(chunk, regionLink)
	r.cur = unsafe.Add(p, n)
	r.end = unsafe.Add(chunk, regionChunk)
	c.Memset(p, 0, size)
	return p
}

// FreeRegion звільняє всі об'єкти регіону r і сам регіон і переносить
// байти користувача з Live(CatRegion) у Freed. nil нічого не робить.
func FreeRegion(rp unsafe.Pointer) {
	countOp(opFreeRegion)
	if rp == nil {
		return
	}
	r := (*region)(rp)
	r.flushProfile()
	s := myAccountShard()
	atomic.Add(&s.live[CatRegion], -uint64(r.noted))
	atomic.Add(&s.freed, uint64(r.bytes))
	freeRegionList(r.blocks)
	// region лежить у першому шматку: список читається до звільнення.
	freeRegionList(r.chunks)
	if regionPerObject {
		FreeRoot(rp)
	}
}

func freeRegionList(p unsafe.Pointer) {
	for p != nil {
		next := *(*unsafe.Pointer)(p)
		FreeRoot(p)
		p = next
	}
}

// flushProfile додає накопичені об'єкти регіону до профілю пам'яті.
func (r *region) flushProfile() {
	for bucket, n := range r.profile {
		if n != 0 {
			memProfileAddObjects(bucket, uint64(n))
			r.profile[bucket] = 0
		}
	}
}

// memProfileBucket — індекс кошика memProfileBuckets для size, як
// memProfileSizeClass: найменший кошик, не менший за size, або останній.
// Кошики — степені двійки від 16.
func memProfileBucket(size uintptr) int {
	i := 0
	for b := memProfileBuckets[0]; b < size && i < len(memProfileBuckets)-1; b <<= 1 {
		i++
	}
	return i
}
