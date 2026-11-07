//go:build !owngo

package runtime

// Вузол очікування належить збирачу.
func allocChanWaiter() *chanWaiter { return new(chanWaiter) }

func freeChanWaiter(*chanWaiter) {}
