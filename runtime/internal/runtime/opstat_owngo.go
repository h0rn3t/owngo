//go:build owngo && owngostat

package runtime

import "github.com/xgo-dev/llgo/runtime/internal/sync/atomic"

// opShard — лічильники операцій потоків одного шарду обліку. 64 байти
// після лічильників відділяють останній від першого лічильника сусіднього
// шарду, тож шарди не ділять кеш-лінію.
type opShard struct {
	ops [opCount]uint64
	_   [64]byte
}

var opShards [accountShardCount]opShard

// opNames — назви рядків owngo ops у порядку номерів операцій.
var opNames = [opCount]string{
	"alloc", "alloc_root", "alloc_counted", "inc", "dec_accounted",
	"dec_cyclic", "cyclic_visit", "inc_shared", "dec_shared",
	"free_accounted", "free_root",
	"note_alloc_static", "note_alloc_rc-local", "note_alloc_rc-shared",
	"note_alloc_cyclic", "note_alloc_pinned", "note_alloc_implicit",
}

func countOp(op int32) {
	atomic.Add(&opShards[myShardIndex()].ops[op], 1)
}

// OpCount повертає кількість викликів операції op за весь процес.
// Номер поза 0…opCount-1 дає 0.
func OpCount(op int32) uint64 {
	if op < 0 || op >= opCount {
		return 0
	}
	var n uint64
	for i := range opShards {
		n += atomic.Load(&opShards[i].ops[op])
	}
	return n
}

func reportOps() {
	for op, name := range opNames {
		println("owngo ops", name, OpCount(int32(op)))
	}
}
