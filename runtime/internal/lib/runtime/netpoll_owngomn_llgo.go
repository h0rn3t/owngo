//go:build owngo && owngomn && go1.23 && (linux || darwin)

// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license.
// See LICENSES/Go-BSD-3-Clause.txt at this module root for license terms.

package runtime

import (
	"sync/atomic"
	"unsafe"

	c "github.com/xgo-dev/llgo/runtime/internal/clite"
	csyscall "github.com/xgo-dev/llgo/runtime/internal/clite/syscall"
	llruntime "github.com/xgo-dev/llgo/runtime/internal/runtime"
	psync "github.com/xgo-dev/llgo/runtime/internal/sync"
)

// Мережевий poller планувальника M:N (P-NEW-09.7) — порт runtime/netpoll.go.
// pollWait паркує G, а не блокує M у poll(2). Готовність дескрипторів
// збирає планувальник: M без роботи блокується в netpoll (epoll чи kqueue),
// mnFindRunnable опитує його без блокування. Дедлайни — таймери рантайму в
// pollDesc, як у Go; таймери живуть на своєму потоці, тож poller чекає без
// таймауту.

// Мають збігатися з кодами internal/poll/fd_poll_runtime.go.
const (
	pollNoError        = 0
	pollErrClosing     = 1
	pollErrTimeout     = 2
	pollErrNotPollable = 3
)

// Стан rg і wg: pdNil, pdReady, pdWait або адреса очікувача rw чи ww того
// самого pollDesc, коли G уже паркується.
const (
	pdNil   uintptr = 0
	pdReady uintptr = 1
	pdWait  uintptr = 2
)

type pollDesc struct {
	link *pollDesc // у кеші вільних
	fd   uintptr
	// everr — подія помилки від poller; читається без замка.
	everr atomic.Bool

	lock    psync.Mutex // захищає поля нижче
	closing bool
	rseq    uintptr // проти застарілих таймерів читання
	rt      runtimeTimer
	rd      int64 // дедлайн читання: 0 — немає, < 0 — минув
	wseq    uintptr
	wt      runtimeTimer
	wd      int64

	rg atomic.Uintptr
	wg atomic.Uintptr
	// Очікувачі — у дескрипторі, а не на стеку G: адреса, що йде в rg/wg,
	// виділила б купу на кожне очікування. Чекає не більше однієї G на
	// напрям (інакше "double wait").
	rw llruntime.SchedulerWaiter
	ww llruntime.SchedulerWaiter
}

var (
	pollCacheLock psync.Mutex
	pollCacheInit psync.Once
	pollCacheHead *pollDesc

	netpollInited atomic.Uint32
	netpollOnce   psync.Once

	// netpollWaiters — G, припарковані в pollWait: поки є, планувальник
	// опитує poller і не приймає сон за взаємоблокування.
	netpollWaiters atomic.Int32
)

func pollCacheSetup() { pollCacheLock.Init(nil) }

// pollCacheAlloc бере pollDesc з кешу. Без GC дескриптори не звільняються:
// закритий повертається в кеш, а застаріла подія poller на ньому дає лише
// зайве пробудження, яке internal/poll переживає.
func pollCacheAlloc() *pollDesc {
	pollCacheInit.Do(pollCacheSetup)
	pollCacheLock.Lock()
	pd := pollCacheHead
	if pd != nil {
		pollCacheHead = pd.link
	}
	pollCacheLock.Unlock()
	if pd == nil {
		pd = new(pollDesc)
		pd.lock.Init(nil)
	}
	return pd
}

func pollCacheFree(pd *pollDesc) {
	pollCacheLock.Lock()
	pd.link = pollCacheHead
	pollCacheHead = pd
	pollCacheLock.Unlock()
}

func netpollGenericInit() {
	if netpollInited.Load() != 0 {
		return
	}
	netpollOnce.Do(netpollSetup)
}

func netpollSetup() {
	netpollInit()
	llruntime.SetNetpollHooks(netpoll, func() bool { return netpollWaiters.Load() > 0 })
	netpollInited.Store(1)
}

//go:linkname poll_runtime_pollServerInit internal/poll.runtime_pollServerInit
func poll_runtime_pollServerInit() { netpollGenericInit() }

//go:linkname poll_runtime_isPollServerDescriptor internal/poll.runtime_isPollServerDescriptor
func poll_runtime_isPollServerDescriptor(fd uintptr) bool { return netpollIsPollDescriptor(fd) }

//go:linkname poll_runtime_pollOpen internal/poll.runtime_pollOpen
func poll_runtime_pollOpen(fd uintptr) (uintptr, int) {
	netpollGenericInit()
	if pollDescriptorUnsupported(c.Int(fd)) {
		// Як epoll: звичайний файл чи каталог — EPERM, і internal/poll
		// лишає його без дескриптора poller (ErrNoDeadline).
		return 0, int(csyscall.EPERM)
	}
	pd := pollCacheAlloc()
	pd.lock.Lock()
	if wg := pd.wg.Load(); wg != pdNil && wg != pdReady {
		throw("runtime: blocked write on free polldesc")
	}
	if rg := pd.rg.Load(); rg != pdNil && rg != pdReady {
		throw("runtime: blocked read on free polldesc")
	}
	pd.fd = fd
	pd.closing = false
	pd.everr.Store(false)
	pd.rseq++
	pd.rg.Store(pdNil)
	pd.rd = 0
	pd.wseq++
	pd.wg.Store(pdNil)
	pd.wd = 0
	pd.lock.Unlock()
	if errno := netpollOpen(fd, pd); errno != 0 {
		pollCacheFree(pd)
		return 0, errno
	}
	return uintptr(unsafe.Pointer(pd)), 0
}

//go:linkname poll_runtime_pollClose internal/poll.runtime_pollClose
func poll_runtime_pollClose(ctx uintptr) {
	pd := (*pollDesc)(unsafe.Pointer(ctx))
	if !pd.closing {
		throw("runtime: close polldesc w/o unblock")
	}
	if wg := pd.wg.Load(); wg != pdNil && wg != pdReady {
		throw("runtime: blocked write on closing polldesc")
	}
	if rg := pd.rg.Load(); rg != pdNil && rg != pdReady {
		throw("runtime: blocked read on closing polldesc")
	}
	netpollClose(pd.fd)
	pollCacheFree(pd)
}

//go:linkname poll_runtime_pollReset internal/poll.runtime_pollReset
func poll_runtime_pollReset(ctx uintptr, mode int) int {
	pd := (*pollDesc)(unsafe.Pointer(ctx))
	if errcode := netpollCheckErr(pd, int32(mode)); errcode != pollNoError {
		return errcode
	}
	if mode == 'r' {
		pd.rg.Store(pdNil)
	} else if mode == 'w' {
		pd.wg.Store(pdNil)
	}
	return pollNoError
}

//go:linkname poll_runtime_pollWait internal/poll.runtime_pollWait
func poll_runtime_pollWait(ctx uintptr, mode int) int {
	pd := (*pollDesc)(unsafe.Pointer(ctx))
	if errcode := netpollCheckErr(pd, int32(mode)); errcode != pollNoError {
		return errcode
	}
	for !netpollBlock(pd, int32(mode), false) {
		if errcode := netpollCheckErr(pd, int32(mode)); errcode != pollNoError {
			return errcode
		}
		// Готовність чи таймаут, що вже пройшли, — повторити очікування.
	}
	return pollNoError
}

//go:linkname poll_runtime_pollWaitCanceled internal/poll.runtime_pollWaitCanceled
func poll_runtime_pollWaitCanceled(ctx uintptr, mode int) {
	// Лише Windows: дочекатися завершення скасованої операції.
	pd := (*pollDesc)(unsafe.Pointer(ctx))
	for !netpollBlock(pd, int32(mode), true) {
	}
}

//go:linkname poll_runtime_pollSetDeadline internal/poll.runtime_pollSetDeadline
func poll_runtime_pollSetDeadline(ctx uintptr, d int64, mode int) {
	pd := (*pollDesc)(unsafe.Pointer(ctx))
	pd.lock.Lock()
	if pd.closing {
		pd.lock.Unlock()
		return
	}
	rd0, wd0 := pd.rd, pd.wd
	combo0 := rd0 > 0 && rd0 == wd0
	if d > 0 {
		d += runtimeNano()
		if d <= 0 {
			d = 1<<63 - 1 // переповнення
		}
	}
	if mode == 'r' || mode == 'r'+'w' {
		pd.rd = d
	}
	if mode == 'w' || mode == 'r'+'w' {
		pd.wd = d
	}
	combo := pd.rd > 0 && pd.rd == pd.wd
	rtf := netpollReadDeadline
	if combo {
		rtf = netpollDeadline
	}
	if pd.rt.f == nil {
		if pd.rd > 0 {
			netpollStartTimer(&pd.rt, pd.rd, rtf, pd, pd.rseq)
		}
	} else if pd.rd != rd0 || combo != combo0 {
		pd.rseq++
		if pd.rd > 0 {
			netpollStartTimer(&pd.rt, pd.rd, rtf, pd, pd.rseq)
		} else {
			stopRuntimeTimer(&pd.rt)
			pd.rt.f = nil
		}
	}
	if pd.wt.f == nil {
		if pd.wd > 0 && !combo {
			netpollStartTimer(&pd.wt, pd.wd, netpollWriteDeadline, pd, pd.wseq)
		}
	} else if pd.wd != wd0 || combo != combo0 {
		pd.wseq++
		if pd.wd > 0 && !combo {
			netpollStartTimer(&pd.wt, pd.wd, netpollWriteDeadline, pd, pd.wseq)
		} else {
			stopRuntimeTimer(&pd.wt)
			pd.wt.f = nil
		}
	}
	// Дедлайн у минулому знімає очікування, що вже йдуть.
	var rw, ww *llruntime.SchedulerWaiter
	if pd.rd < 0 {
		rw = netpollUnblock(pd, 'r', false)
	}
	if pd.wd < 0 {
		ww = netpollUnblock(pd, 'w', false)
	}
	pd.lock.Unlock()
	netpollReadyWaiter(rw)
	netpollReadyWaiter(ww)
}

//go:linkname poll_runtime_pollUnblock internal/poll.runtime_pollUnblock
func poll_runtime_pollUnblock(ctx uintptr) {
	pd := (*pollDesc)(unsafe.Pointer(ctx))
	pd.lock.Lock()
	if pd.closing {
		throw("runtime: unblock on closing polldesc")
	}
	pd.closing = true
	pd.rseq++
	pd.wseq++
	rw := netpollUnblock(pd, 'r', false)
	ww := netpollUnblock(pd, 'w', false)
	if pd.rt.f != nil {
		stopRuntimeTimer(&pd.rt)
		pd.rt.f = nil
	}
	if pd.wt.f != nil {
		stopRuntimeTimer(&pd.wt)
		pd.wt.f = nil
	}
	pd.lock.Unlock()
	netpollReadyWaiter(rw)
	netpollReadyWaiter(ww)
}

// netpollStartTimer ставить таймер дедлайну. Поля читає знімок у
// resetRuntimeTimer під замком планувальника таймерів.
func netpollStartTimer(t *runtimeTimer, when int64, f func(any, uintptr, int64), pd *pollDesc, seq uintptr) {
	t.f = f
	t.arg = pd
	t.seq = seq
	resetRuntimeTimer(t, when, 0, nil)
}

// netpollReady — подія poller для pd: mode — 'r', 'w' чи 'r'+'w'.
func netpollReady(pd *pollDesc, mode int32) {
	var rw, ww *llruntime.SchedulerWaiter
	if mode == 'r' || mode == 'r'+'w' {
		rw = netpollUnblock(pd, 'r', true)
	}
	if mode == 'w' || mode == 'r'+'w' {
		ww = netpollUnblock(pd, 'w', true)
	}
	netpollReadyWaiter(rw)
	netpollReadyWaiter(ww)
}

func netpollCheckErr(pd *pollDesc, mode int32) int {
	pd.lock.Lock()
	defer pd.lock.Unlock()
	if pd.closing {
		return pollErrClosing
	}
	if mode == 'r' && pd.rd < 0 || mode == 'w' && pd.wd < 0 {
		return pollErrTimeout
	}
	// Помилку події повідомляємо лише читанню: запис і так побачить її
	// з наступного системного виклику.
	if mode == 'r' && pd.everr.Load() {
		return pollErrNotPollable
	}
	return pollNoError
}

// netpollBlock паркує G до готовності pd на mode. true — дескриптор
// готовий, false — пробудження через помилку, дедлайн чи закриття.
// waitio — чекати лише готовності, ігноруючи помилки.
func netpollBlock(pd *pollDesc, mode int32, waitio bool) bool {
	gpp, w := &pd.rg, &pd.rw
	if mode == 'w' {
		gpp, w = &pd.wg, &pd.ww
	}
	for {
		if gpp.CompareAndSwap(pdReady, pdNil) {
			return true
		}
		if gpp.CompareAndSwap(pdNil, pdWait) {
			break
		}
		if v := gpp.Load(); v != pdReady && v != pdNil {
			throw("runtime: double wait")
		}
	}
	// Помилку перевіряємо після pdWait: netpollUnblock, що прийшов між
	// ними, уже побачив pdWait і змінив його.
	if waitio || netpollCheckErr(pd, mode) == pollNoError {
		// Ready раніше за Park SchedulerWaiter не губить, а попередній Ready
		// після свого CAS уже не читає *w.
		*w = llruntime.CurrentSchedulerWaiter()
		if gpp.CompareAndSwap(pdWait, uintptr(unsafe.Pointer(w))) {
			netpollWaiters.Add(1)
			w.Park()
			netpollWaiters.Add(-1)
		}
	}
	old := gpp.Swap(pdNil)
	if old > pdWait {
		throw("runtime: corrupted polldesc")
	}
	return old == pdReady
}

// netpollUnblock знімає очікувача pd на mode і повертає його, якщо той уже
// припаркований. ioready — дескриптор готовий.
func netpollUnblock(pd *pollDesc, mode int32, ioready bool) *llruntime.SchedulerWaiter {
	gpp := &pd.rg
	if mode == 'w' {
		gpp = &pd.wg
	}
	for {
		old := gpp.Load()
		if old == pdReady {
			return nil
		}
		if old == pdNil && !ioready {
			return nil
		}
		next := pdNil
		if ioready {
			next = pdReady
		}
		if gpp.CompareAndSwap(old, next) {
			if old == pdWait {
				old = pdNil
			}
			if old == pdNil {
				return nil
			}
			return (*llruntime.SchedulerWaiter)(unsafe.Pointer(old))
		}
	}
}

func netpollReadyWaiter(w *llruntime.SchedulerWaiter) {
	if w != nil {
		w.Ready()
	}
}

func netpollDeadlineImpl(pd *pollDesc, seq uintptr, read, write bool) {
	pd.lock.Lock()
	current := pd.rseq
	if !read {
		current = pd.wseq
	}
	if seq != current {
		pd.lock.Unlock() // дескриптор перевикористано або таймер скинуто
		return
	}
	var rw, ww *llruntime.SchedulerWaiter
	if read {
		if pd.rd <= 0 || pd.rt.f == nil {
			throw("runtime: inconsistent read deadline")
		}
		pd.rd = -1
		pd.rt.f = nil
		rw = netpollUnblock(pd, 'r', false)
	}
	if write {
		if pd.wd <= 0 || pd.wt.f == nil && !read {
			throw("runtime: inconsistent write deadline")
		}
		pd.wd = -1
		pd.wt.f = nil
		ww = netpollUnblock(pd, 'w', false)
	}
	pd.lock.Unlock()
	netpollReadyWaiter(rw)
	netpollReadyWaiter(ww)
}

func netpollDeadline(arg any, seq uintptr, _ int64) {
	netpollDeadlineImpl(arg.(*pollDesc), seq, true, true)
}

func netpollReadDeadline(arg any, seq uintptr, _ int64) {
	netpollDeadlineImpl(arg.(*pollDesc), seq, true, false)
}

func netpollWriteDeadline(arg any, seq uintptr, _ int64) {
	netpollDeadlineImpl(arg.(*pollDesc), seq, false, true)
}
