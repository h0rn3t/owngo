//go:build owngo && owngomn && go1.23 && darwin

// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license.
// See LICENSES/Go-BSD-3-Clause.txt at this module root for license terms.

package runtime

import (
	"unsafe"

	c "github.com/xgo-dev/llgo/runtime/internal/clite"
	cliteos "github.com/xgo-dev/llgo/runtime/internal/clite/os"
	csyscall "github.com/xgo-dev/llgo/runtime/internal/clite/syscall"
)

const (
	evfiltRead  = -1
	evfiltWrite = -2
	evAdd       = 0x1
	evClear     = 0x20
	evError     = 0x4000
)

// keventT — struct kevent macOS для 64-бітних цілей.
type keventT struct {
	ident  uintptr
	filter int16
	flags  uint16
	fflags uint32
	data   int64
	udata  uintptr
}

type kqTimespec struct {
	sec  int64
	nsec int64
}

//go:linkname c_kqueue C.kqueue
func c_kqueue() c.Int

//go:linkname c_kevent C.kevent
func c_kevent(kq c.Int, changes *keventT, nchanges c.Int, events *keventT, nevents c.Int, timeout *kqTimespec) c.Int

var kq c.Int = -1

func netpollInit() {
	kq = c_kqueue()
	if kq < 0 {
		throw("runtime: netpollinit failed")
	}
	flags := cliteos.Fcntl(kq, c.Int(csyscall.F_GETFD))
	if flags >= 0 {
		cliteos.Fcntl(kq, c.Int(csyscall.F_SETFD), uintptr(flags)|uintptr(csyscall.FD_CLOEXEC))
	}
}

func netpollIsPollDescriptor(fd uintptr) bool { return fd == uintptr(kq) }

// netpollOpen реєструє fd по краю (EV_CLEAR) на обидва напрями, як Go.
func netpollOpen(fd uintptr, pd *pollDesc) int {
	var ev [2]keventT
	ev[0] = keventT{ident: fd, filter: evfiltRead, flags: evAdd | evClear, udata: uintptr(unsafe.Pointer(pd))}
	ev[1] = ev[0]
	ev[1].filter = evfiltWrite
	if c_kevent(kq, &ev[0], 2, nil, 0, nil) < 0 {
		return int(cliteos.Errno())
	}
	return 0
}

// netpollClose нічого не робить: close(fd) сам знімає події з kqueue.
func netpollClose(uintptr) {}

// netpoll перевіряє готовність дескрипторів і будить їхніх очікувачів.
// delay < 0 — чекати без межі, 0 — не блокуватися, > 0 — до delay нс.
func netpoll(delay int64) {
	var ts kqTimespec
	tp := &ts
	switch {
	case delay < 0:
		tp = nil
	case delay > 1e15:
		ts.sec = 1e6 // близько 11.5 доби
	default:
		ts.sec, ts.nsec = delay/1e9, delay%1e9
	}
	var events [64]keventT
	for {
		n := c_kevent(kq, nil, 0, &events[0], c.Int(len(events)), tp)
		if n < 0 {
			if cliteos.Errno() != c.Int(csyscall.EINTR) {
				throw("runtime: netpoll failed")
			}
			if delay > 0 {
				return // викликач перерахує час очікування
			}
			continue
		}
		for i := c.Int(0); i < n; i++ {
			ev := &events[i]
			var mode int32
			switch ev.filter {
			case evfiltRead:
				mode = 'r'
			case evfiltWrite:
				mode = 'w'
			default:
				continue
			}
			pd := (*pollDesc)(unsafe.Pointer(ev.udata))
			pd.everr.Store(ev.filter == evfiltRead && ev.flags&evError != 0)
			netpollReady(pd, mode)
		}
		return
	}
}
