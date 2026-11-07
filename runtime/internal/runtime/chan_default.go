//go:build !owngo

package runtime

// Заголовок, буфер і вузол очікування належать збирачу.
func allocChan(mem uintptr, cap, eltAlign int) *Chan {
	ret := new(Chan)
	if cap > 0 {
		ret.buf = AllocUAligned(mem, uintptr(eltAlign))
	}
	return ret
}

func allocChanWaiter() *chanWaiter { return new(chanWaiter) }

func freeChanWaiter(*chanWaiter) {}
