//go:build owngo && owngomn && go1.23 && linux && amd64

package runtime

// epollEvent — struct epoll_event на amd64: упакована, data з зсувом 4.
type epollEvent struct {
	events uint32
	lo, hi uint32
}

func (ev *epollEvent) setData(v uintptr) { ev.lo, ev.hi = uint32(v), uint32(v>>32) }

func (ev *epollEvent) data() uintptr { return uintptr(ev.lo) | uintptr(ev.hi)<<32 }
