//go:build llgo && !baremetal && !wasm

package runtime

import "github.com/xgo-dev/llgo/runtime/internal/sync/atomic"

// memProfileShard holds the object counts of the threads assigned to it.
// The trailing pad keeps the counters of adjacent shards more than 64 bytes
// apart, so at any alignment of the array they never share a cache line.
type memProfileShard struct {
	objects [len(memProfileBuckets)]uint64
	_       [64]byte
}

// Threads beyond memProfileShardCount share shards round-robin, so the
// increments stay atomic; what sharding removes is the cross-core cache-line
// traffic of a single counter hit by every allocation.
const memProfileShardCount = 64

var (
	memProfileShards    [memProfileShardCount]memProfileShard
	nextMemProfileShard uint32
)

// currentMemProfileShard is the thread's shard number plus one; 0 means not
// assigned yet. It holds a number rather than a pointer so the TLS access
// stays a direct thread-pointer load.
//
//llgointernal:tls
var currentMemProfileShard uint32

func memProfileAddObject(bucket int) {
	i := currentMemProfileShard
	if i == 0 {
		i = atomic.Add(&nextMemProfileShard, 1)%memProfileShardCount + 1
		currentMemProfileShard = i
	}
	atomic.Add(&memProfileShards[i-1].objects[bucket], 1)
}

func memProfileLoadObjects(bucket int) uint64 {
	var n uint64
	for i := range memProfileShards {
		n += atomic.Load(&memProfileShards[i].objects[bucket])
	}
	return n
}
