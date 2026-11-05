//go:build owngo

// Програма прийняття P-NEW-05.3: рантайм регіону. Код 0 лише якщо
// обнулення, вирівнювання, незалежність регіонів, облік категорії region
// і пікова RSS під повторними циклами — як у плані. Аргумент n — кількість
// циклів «створити, 1000 алокацій, звільнити», за замовчуванням 1 000 000;
// пікова RSS порівнюється після n/10 і після n циклів.
package main

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

const catRegion = 6 // runtime.CatRegion

//go:linkname regionCreate github.com/xgo-dev/llgo/runtime/internal/runtime.RegionCreate
func regionCreate() unsafe.Pointer

//go:linkname regionAlloc github.com/xgo-dev/llgo/runtime/internal/runtime.RegionAlloc
func regionAlloc(r unsafe.Pointer, size uintptr) unsafe.Pointer

//go:linkname freeRegion github.com/xgo-dev/llgo/runtime/internal/runtime.FreeRegion
func freeRegion(r unsafe.Pointer)

//go:linkname freed github.com/xgo-dev/llgo/runtime/internal/runtime.Freed
func freed() uint64

//go:linkname live github.com/xgo-dev/llgo/runtime/internal/runtime.Live
func live(cat int32) uint64

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func fill(p unsafe.Pointer, size uintptr, v byte) {
	b := unsafe.Slice((*byte)(p), size)
	for i := range b {
		b[i] = v
	}
}

// check перевіряє, що всі байти блока — v; what і id називають блок
// у повідомленні. Рядок не збирається заздалегідь: звільнення змінило б Freed.
func check(p unsafe.Pointer, size uintptr, v byte, what string, id uintptr) {
	for i, b := range unsafe.Slice((*byte)(p), size) {
		if b != v {
			fail("%s %d: byte %d = %#x, want %#x", what, id, i, b, v)
		}
	}
}

// Розміри плану: 0, 1, 8, 24, 4096 і більший за шматок.
var sizes = []uintptr{0, 1, 8, 24, 4096, 1 << 20}

// zeroAlign перевіряє обнулення на пам'яті, яку попередній регіон
// заповнив ненульовими байтами, і вирівнювання на 16.
func zeroAlign() {
	dirty := regionCreate()
	for range 4 {
		for _, size := range sizes {
			fill(regionAlloc(dirty, size), size, 0xa5)
		}
	}
	freeRegion(dirty)

	r := regionCreate()
	zero := regionAlloc(r, 0)
	if zero == nil || regionAlloc(r, 0) != zero {
		fail("RegionAlloc(0) = %p, want one shared non-nil base", zero)
	}
	for range 4 {
		for _, size := range sizes[1:] {
			p := regionAlloc(r, size)
			if uintptr(p)%16 != 0 {
				fail("RegionAlloc(%d) = %p, want 16-byte alignment", size, p)
			}
			check(p, size, 0, "RegionAlloc", size)
			fill(p, size, 0x5a)
		}
	}
	freeRegion(r)
}

type block struct {
	p    unsafe.Pointer
	size uintptr
}

// outOfOrder звільняє три регіони не в порядку створення: вміст живих
// не змінюється.
func outOfOrder() {
	var regions [3]unsafe.Pointer
	var blocks [3][]block
	for i := range regions {
		regions[i] = regionCreate()
		for j := range 200 {
			size := uintptr(8 + 40*j%3000)
			p := regionAlloc(regions[i], size)
			fill(p, size, byte(i+1))
			blocks[i] = append(blocks[i], block{p, size})
		}
	}
	verify := func(i int) {
		for _, b := range blocks[i] {
			check(b.p, b.size, byte(i+1), "region", uintptr(i))
		}
	}
	freeRegion(regions[1])
	verify(0)
	verify(2)
	freeRegion(regions[0])
	verify(2)
	freeRegion(regions[2])
}

// profileBuf — записи runtime.MemProfile; масив глобальний, щоб знімок
// сам нічого не виділяв між знімками.
var profileBuf [64]runtime.MemProfileRecord

// profileObjects повертає AllocObjects профілю пам'яті за log2 розміру кошика.
func profileObjects() (objects [64]int64) {
	// Обгортка runtime.MemProfile рахує записи, а потім сама виділяє масив,
	// і новий кошик цього масиву може не вміститись: тоді ok == false
	// (бачено під -asan). Другий виклик уже бачить цей кошик.
	n, ok := runtime.MemProfile(profileBuf[:], true)
	if !ok {
		n, ok = runtime.MemProfile(profileBuf[:], true)
	}
	if !ok {
		fail("MemProfile: %d records do not fit", n)
	}
	for _, r := range profileBuf[:n] {
		if r.AllocObjects > 0 {
			objects[log2Bucket(uintptr(r.AllocBytes/r.AllocObjects))] += r.AllocObjects
		}
	}
	return objects
}

// log2Bucket — log2 кошика профілю для об'єкта size: найменший степінь
// двійки, не менший за size і за 16.
func log2Bucket(size uintptr) int {
	k := 4
	for uintptr(1)<<k < size {
		k++
	}
	return k
}

// accounting: Freed росте на байти користувача регіону, Live категорії
// region після звільнення — як до створення, а поки регіон живий — не
// більше за його байти. Профіль пам'яті після FreeRegion рахує кожен
// об'єкт регіону у своєму кошику.
func accounting() {
	// Два знімки поспіль дають внесок самого виклику MemProfile.
	first := profileObjects()
	beforeProfile := profileObjects()
	var wantProfile [64]int64
	for k := range wantProfile {
		wantProfile[k] = beforeProfile[k] - first[k]
	}
	beforeFreed, beforeLive := freed(), live(catRegion)
	r := regionCreate()
	var want uint64
	for j := range 5000 {
		size := uintptr(1 + j%300)
		regionAlloc(r, size)
		want += uint64(size)
		wantProfile[log2Bucket(size)]++
	}
	regionAlloc(r, 1<<20)
	want += 1 << 20
	wantProfile[20]++
	if got := live(catRegion) - beforeLive; got == 0 || got > want {
		fail("live region while alive: delta %d, want 1…%d", got, want)
	}
	freeRegion(r)
	if got := freed() - beforeFreed; got != want {
		fail("freed delta %d, want %d", got, want)
	}
	if got := live(catRegion); got != beforeLive {
		fail("live region %d after FreeRegion, want %d", got, beforeLive)
	}
	afterProfile := profileObjects()
	for k := range afterProfile {
		if got := afterProfile[k] - beforeProfile[k]; got != wantProfile[k] {
			fail("MemProfile bucket %d bytes: %d objects, want %d", 1<<k, got, wantProfile[k])
		}
	}
}

// Бар'єри — атомарні змінні: між знімками горутини лише працюють
// з регіонами, тож у різницю Freed не потрапляє звільнення замикань.
var ready, finished, start, release atomic.Int32

func wait(flag *atomic.Int32, want int32) {
	for flag.Load() != want {
		runtime.Gosched()
	}
}

// goroutines: вісім горутин зі своїми регіонами; підсумок Freed точний.
func goroutines() {
	const workers, rounds, allocs = 8, 200, 300
	var want uint64
	for g := range workers {
		for j := range allocs {
			want += uint64(rounds) * uint64(1+(g+j)%64)
		}
	}
	var wg sync.WaitGroup
	for g := range workers {
		wg.Go(func() {
			blocks := make([]block, allocs)
			ready.Add(1)
			wait(&start, 1)
			for range rounds {
				r := regionCreate()
				for j := range blocks {
					size := uintptr(1 + (g+j)%64)
					p := regionAlloc(r, size)
					fill(p, size, byte(g+1))
					blocks[j] = block{p, size}
				}
				for _, b := range blocks {
					check(b.p, b.size, byte(g+1), "goroutine", uintptr(g))
				}
				freeRegion(r)
			}
			finished.Add(1)
			wait(&release, 1)
		})
	}
	wait(&ready, workers)
	beforeFreed := freed()
	start.Store(1)
	wait(&finished, workers)
	afterFreed := freed()
	release.Store(1)
	wg.Wait()
	if got := afterFreed - beforeFreed; got != want {
		fail("goroutines: freed delta %d, want %d", got, want)
	}
}

// maxRSS повертає пікову RSS процесу в КіБ.
func maxRSS() int64 {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		fail("getrusage: %v", err)
	}
	if runtime.GOOS == "darwin" {
		return ru.Maxrss / 1024 // на darwin — байти
	}
	return ru.Maxrss
}

func cycles(n int) {
	for range n {
		r := regionCreate()
		for range 1000 {
			*(*uint64)(regionAlloc(r, 24)) = 1
		}
		freeRegion(r)
	}
}

func main() {
	n := 1_000_000
	if len(os.Args) > 1 {
		var err error
		if n, err = strconv.Atoi(os.Args[1]); err != nil || n < 10 {
			fmt.Fprintln(os.Stderr, "bad n", os.Args[1])
			os.Exit(2)
		}
	}
	zeroAlign()
	outOfOrder()
	accounting()
	goroutines()

	cycles(n / 10)
	early := maxRSS()
	cycles(n - n/10)
	if late := maxRSS(); late-early > 1024 {
		fail("peak RSS grew from %d KiB after %d cycles to %d KiB after %d", early, n/10, late, n)
	}
	println("region-ok", n)
}
