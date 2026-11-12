//go:build owngo && owngostat

package runtime

import "github.com/xgo-dev/llgo/runtime/internal/sync/atomic"

// shardSeen — покоління, у якому шард востаннє віддав індекс. ResetUsedShards
// збільшує shardGen, тож UsedShards бачить лише звернення після скидання.
var (
	shardGen  uint32
	shardSeen [accountShardCount]uint32
)

func noteShard(i uint32) {
	atomic.Store(&shardSeen[i], atomic.Load(&shardGen))
}

// ResetUsedShards починає нове вікно підрахунку задіяних шардів.
func ResetUsedShards() {
	atomic.Add(&shardGen, 1)
}

// UsedShards — скільки шардів віддали індекс після останнього ResetUsedShards.
func UsedShards() int {
	gen := atomic.Load(&shardGen)
	if gen == 0 {
		return 0
	}
	n := 0
	for i := range shardSeen {
		if atomic.Load(&shardSeen[i]) == gen {
			n++
		}
	}
	return n
}

// MyShardForTesting — індекс шарду цього потоку.
func MyShardForTesting() uint32 { return myShardIndex() }
