//go:build !baremetal && (!llgo || wasm)

package runtime

import "github.com/xgo-dev/llgo/runtime/internal/sync/atomic"

var memProfileObjects [len(memProfileBuckets)]uint64

func memProfileAddObject(bucket int) {
	atomic.Add(&memProfileObjects[bucket], 1)
}

func memProfileAddObjects(bucket int, n uint64) {
	atomic.Add(&memProfileObjects[bucket], n)
}

func memProfileLoadObjects(bucket int) uint64 {
	return atomic.Load(&memProfileObjects[bucket])
}
