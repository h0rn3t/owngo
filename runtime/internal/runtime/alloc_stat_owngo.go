//go:build owngo && !owngoasan && (linux || darwin) && owngostat

package runtime

import "unsafe"

// З тегом owngostat алокатор збирається з лічильниками (P-NEW-08.1).
const owngoAllocCFile = "_wrap/owngo_alloc_stat.c"

// allocStatNames — назви рядків owngo alloc у порядку лічильників
// owngo_alloc_stat (enum SC_STAT_* у _wrap/owngo_alloc.c).
var allocStatNames = [...]string{
	"small", "large", "free_local", "free_remote", "free_large",
	"page_new", "page_reuse", "page_give", "madvise", "madvise_bytes", "lock",
}

//go:linkname owngoAllocStat C.owngo_alloc_stat
func owngoAllocStat(out unsafe.Pointer)

// allocStat — глобальна змінна, а не локальна: локальний масив під
// unsafe.Pointer став би алокацією pinned між рядками owngo ops і owngo
// site і розійшовся б з ними.
var allocStat [len(allocStatNames)]uint64

func reportAlloc() {
	owngoAllocStat(unsafe.Pointer(&allocStat[0]))
	for i, name := range allocStatNames {
		println("owngo alloc", name, allocStat[i])
	}
}
