//go:build owngo && owngomn && go1.23 && linux && arm64

package runtime

// epollEvent — struct epoll_event на arm64: data вирівняна на 8.
type epollEvent struct {
	events uint32
	_      uint32
	v      uintptr
}

func (ev *epollEvent) setData(v uintptr) { ev.v = v }

func (ev *epollEvent) data() uintptr { return ev.v }
