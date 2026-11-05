//go:build owngo && owngostat

package runtime

import (
	"unsafe"

	"github.com/xgo-dev/llgo/runtime/internal/sync/atomic"
)

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
	"region_create", "alloc_region", "free_region",
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
	reportSites()
}

// siteRow і siteTable — розкладка таблиці owngo site, яку компілятор дає
// кожному пакету під owngostat (cl.ownGoSiteTable): лічильник і текст
// «file:line категорія причина операція».
type siteRow struct {
	n    *uint64
	text string
}

type siteTable struct {
	next *siteTable
	rows []siteRow
}

var siteTables *siteTable

// RegisterSites додає таблицю пакета до списку. Init пакета кличе його один
// раз; повторний виклик з тією самою таблицею нічого не змінює.
func RegisterSites(t *siteTable) {
	for s := siteTables; s != nil; s = s.next {
		if s == t {
			return
		}
	}
	t.next = siteTables
	siteTables = t
}

// siteCount — рядок owngo site зі знятим значенням лічильника.
type siteCount struct {
	n    uint64
	text string
}

// reportSites друкує рядки owngo site з ненульовим лічильником за спаданням
// n. Рядки з тим самим текстом з різних пакетів (екземпляри узагальнених
// функцій) зливаються в один. Буфер — AllocRoot, а не append: операція
// місця тут змінила б лічильники, які звіт саме читає.
func reportSites() {
	total := 0
	for t := siteTables; t != nil; t = t.next {
		total += len(t.rows)
	}
	if total == 0 {
		return
	}
	buf := AllocRoot(uintptr(total) * unsafe.Sizeof(siteCount{}))
	rows := unsafe.Slice((*siteCount)(buf), total)[:0]
	for t := siteTables; t != nil; t = t.next {
		for _, r := range t.rows {
			if n := atomic.Load(r.n); n != 0 {
				rows = rows[:len(rows)+1]
				rows[len(rows)-1] = siteCount{n, r.text}
			}
		}
	}
	sortSites(rows, func(a, b *siteCount) bool { return a.text < b.text })
	merged := rows[:0]
	for _, r := range rows {
		if last := len(merged) - 1; last >= 0 && merged[last].text == r.text {
			merged[last].n += r.n
			continue
		}
		merged = merged[:len(merged)+1]
		merged[len(merged)-1] = r
	}
	sortSites(merged, func(a, b *siteCount) bool {
		return a.n > b.n || a.n == b.n && a.text < b.text
	})
	for _, r := range merged {
		println("owngo site", r.text, r.n)
	}
	FreeRoot(buf)
}

// sortSites — сортування Шелла: пакет runtime не імпортує sort.
func sortSites(rows []siteCount, less func(a, b *siteCount) bool) {
	for gap := len(rows) / 2; gap > 0; gap /= 2 {
		for i := gap; i < len(rows); i++ {
			for j := i; j >= gap && less(&rows[j], &rows[j-gap]); j -= gap {
				rows[j], rows[j-gap] = rows[j-gap], rows[j]
			}
		}
	}
}
