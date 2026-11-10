//go:build owngo && owngomn && go1.23 && linux

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
	epollIn    = 0x1
	epollOut   = 0x4
	epollErr   = 0x8
	epollHup   = 0x10
	epollRdHup = 0x2000
	epollET    = 0x80000000

	epollCtlAdd = 1
	epollCtlDel = 2

	epollCloexec = 0x80000
)

//go:linkname c_epoll_create1 C.epoll_create1
func c_epoll_create1(flags c.Int) c.Int

//go:linkname c_epoll_ctl C.epoll_ctl
func c_epoll_ctl(epfd, op, fd c.Int, ev *epollEvent) c.Int

//go:linkname c_epoll_wait C.epoll_wait
func c_epoll_wait(epfd c.Int, evs *epollEvent, maxevents, timeout c.Int) c.Int

var epfd c.Int = -1

func netpollInit() {
	epfd = c_epoll_create1(epollCloexec)
	if epfd < 0 {
		throw("runtime: netpollinit failed")
	}
}

func netpollIsPollDescriptor(fd uintptr) bool { return fd == uintptr(epfd) }

// netpollOpen реєструє fd по краю на обидва напрями, як Go.
func netpollOpen(fd uintptr, pd *pollDesc) int {
	var ev epollEvent
	ev.events = epollIn | epollOut | epollRdHup | epollET
	ev.setData(uintptr(unsafe.Pointer(pd)))
	if c_epoll_ctl(epfd, epollCtlAdd, c.Int(fd), &ev) != 0 {
		return int(cliteos.Errno())
	}
	return 0
}

func netpollClose(fd uintptr) {
	var ev epollEvent
	c_epoll_ctl(epfd, epollCtlDel, c.Int(fd), &ev)
}

// netpoll перевіряє готовність дескрипторів і будить їхніх очікувачів.
// delay < 0 — чекати без межі, 0 — не блокуватися, > 0 — до delay нс.
func netpoll(delay int64) {
	waitms := c.Int(-1)
	switch {
	case delay == 0:
		waitms = 0
	case delay > 0 && delay < 1e6:
		waitms = 1
	case delay > 0 && delay < 1e15:
		waitms = c.Int(delay / 1e6)
	case delay > 0:
		waitms = 1e9 // близько 11.5 доби
	}
	var events [128]epollEvent
	for {
		n := c_epoll_wait(epfd, &events[0], c.Int(len(events)), waitms)
		if n < 0 {
			if cliteos.Errno() != c.Int(csyscall.EINTR) {
				throw("runtime: netpoll failed")
			}
			if waitms > 0 {
				return // викликач перерахує час очікування
			}
			continue
		}
		for i := c.Int(0); i < n; i++ {
			ev := &events[i]
			if ev.events == 0 {
				continue
			}
			var mode int32
			if ev.events&(epollIn|epollRdHup|epollHup|epollErr) != 0 {
				mode += 'r'
			}
			if ev.events&(epollOut|epollHup|epollErr) != 0 {
				mode += 'w'
			}
			if mode == 0 {
				continue
			}
			pd := (*pollDesc)(unsafe.Pointer(ev.data()))
			pd.everr.Store(false)
			if ev.events == epollErr {
				pd.everr.Store(true)
			}
			netpollReady(pd, mode)
		}
		return
	}
}
