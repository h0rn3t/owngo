//go:build owngo && owngomn && owngostat

// Програма прийняття P-NEW-09.9: 8 горутин на GOMAXPROCS=2 після
// C.usleep лишаються на своїх M (вони крутяться в C, доки main не
// відкриє хвіртку) і там алокують та звільняють. Код 0 лише якщо різних
// шардів не більше за GOMAXPROCS, а зміна owngo freed, owngo live static
// і owngo ops alloc точна. До прив'язки шарду до P кожна M отримує свій.
package main

/*
#include <unistd.h>
#include <stdatomic.h>
static _Atomic int mn_gate;
void mnWaitGate(int want) {
	while (atomic_load(&mn_gate) < want) {
	}
}
void mnOpenGate(int v) { atomic_store(&mn_gate, v); }
*/
import "C"

import (
	"fmt"
	"os"
	"runtime"
	"sync/atomic"
	"unsafe"
)

const (
	procs   = 2
	workers = 8
	size    = 64
)

//go:linkname allocU github.com/xgo-dev/llgo/runtime/internal/runtime.AllocU
func allocU(size uintptr) unsafe.Pointer

//go:linkname noteAlloc github.com/xgo-dev/llgo/runtime/internal/runtime.NoteAlloc
func noteAlloc(size uintptr, cat int32)

//go:linkname freeAccounted github.com/xgo-dev/llgo/runtime/internal/runtime.FreeAccounted
func freeAccounted(ptr unsafe.Pointer, size uintptr, cat int32)

//go:linkname freed github.com/xgo-dev/llgo/runtime/internal/runtime.Freed
func freed() uint64

//go:linkname live github.com/xgo-dev/llgo/runtime/internal/runtime.Live
func live(cat int32) uint64

//go:linkname opCount github.com/xgo-dev/llgo/runtime/internal/runtime.OpCount
func opCount(op int32) uint64

//go:linkname resetUsedShards github.com/xgo-dev/llgo/runtime/internal/runtime.ResetUsedShards
func resetUsedShards()

//go:linkname usedShards github.com/xgo-dev/llgo/runtime/internal/runtime.UsedShards
func usedShards() int

//go:linkname myShard github.com/xgo-dev/llgo/runtime/internal/runtime.MyShardForTesting
func myShard() uint32

//go:linkname procID github.com/xgo-dev/llgo/runtime/internal/runtime.ProcIDForTesting
func procID() int32

//go:linkname mnCount github.com/xgo-dev/llgo/runtime/internal/runtime.MNCountForTesting
func mnCount() int

func wait(flag *atomic.Int32, want int32) {
	for flag.Load() != want {
		runtime.Gosched()
	}
}

func main() {
	runtime.GOMAXPROCS(procs)
	var phase, entered, done atomic.Int32
	shards := make([]uint32, workers)
	pids := make([]int32, workers)
	for i := range workers {
		go func() {
			entered.Add(1)
			wait(&phase, 1)
			C.usleep(100000)
			C.mnWaitGate(1)
			ptr := allocU(size)
			noteAlloc(size, 0)
			freeAccounted(ptr, size, 0)
			shards[i] = myShard()
			pids[i] = procID()
			done.Add(1)
			C.mnWaitGate(2)
		}()
	}
	wait(&entered, workers)
	phase.Store(1)
	C.usleep(200000)
	resetUsedShards()
	beforeFreed, beforeLive, beforeAlloc, beforeNote := freed(), live(0), opCount(0), opCount(11)
	C.mnOpenGate(1)
	wait(&done, workers)
	gotFreed := freed() - beforeFreed
	gotNote := opCount(11) - beforeNote
	gotLive := live(0)
	gotAlloc := opCount(0) - beforeAlloc
	uniq := map[uint32]struct{}{}
	for _, id := range shards {
		uniq[id] = struct{}{}
	}
	// ops alloc на виході з C рахує ще планувальник; вісім AllocU — нижня межа.
	// Точні — freed, live і note_alloc static.
	ms := mnCount()
	used := usedShards()
	held := 0
	for i, id := range shards {
		if pids[i] < 0 {
			continue
		}
		held++
		if uint32(pids[i])%64 != id {
			fmt.Fprintf(os.Stderr, "mn-shards: shard %d != P %d\n", id, pids[i])
			os.Exit(1)
		}
	}
	if ms <= procs || len(uniq) > procs || used > procs || gotFreed != uint64(workers*size) || gotLive != beforeLive || gotNote != uint64(workers) || gotAlloc < uint64(workers) {
		fmt.Fprintf(os.Stderr, "mn-shards: M %d held %d uniq %d used %d shards %v pids %v freed %d want %d alloc %d note %d live %d want %d\n",
			ms, held, len(uniq), used, shards, pids, gotFreed, workers*size, gotAlloc, gotNote, gotLive, beforeLive)
		os.Exit(1)
	}
	fmt.Printf("mn-shards used %d freed %d alloc %d shards %v pids %v\n", len(uniq), gotFreed, gotAlloc, shards, pids)
	C.mnOpenGate(2)
}
