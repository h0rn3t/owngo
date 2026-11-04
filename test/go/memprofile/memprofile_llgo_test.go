//go:build llgo

package memprofile

import (
	"runtime"
	"sync"
	"testing"
)

// pair is 16 bytes, so every allocation lands in the 16-byte bucket.
type pair struct{ a, b *int }

var pairSinks [8][]*pair

// llgo counts every allocation, so concurrent allocations must all show up.
// gc samples per P and may miss the first allocations after a rate change.
func TestRuntimeMemProfileCountsConcurrentAllocations(t *testing.T) {
	const n = 20000
	before := allocObjects16(readMemProfile(t))

	var wg sync.WaitGroup
	for g := range pairSinks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sink := make([]*pair, 0, n)
			for i := 0; i < n; i++ {
				sink = append(sink, new(pair))
			}
			pairSinks[g] = sink
		}()
	}
	wg.Wait()

	got := allocObjects16(readMemProfile(t)) - before
	if want := int64(len(pairSinks) * n); got < want {
		t.Fatalf("MemProfile 16-byte AllocObjects grew by %d, want >= %d", got, want)
	}
}

func allocObjects16(records []runtime.MemProfileRecord) int64 {
	var sum int64
	for _, r := range records {
		if r.AllocObjects > 0 && r.AllocBytes/r.AllocObjects == 16 {
			sum += r.AllocObjects
		}
	}
	return sum
}
