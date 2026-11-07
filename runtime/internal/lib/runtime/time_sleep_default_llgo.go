//go:build !owngo && !baremetal

package runtime

// Канал time.Sleep належить збирачу.
func freeSleepChan(chan struct{}) {}
