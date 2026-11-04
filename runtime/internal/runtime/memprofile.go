package runtime

// MemProfileRecord describes allocations aggregated by size class.
type MemProfileRecord struct {
	ObjectSize                int64
	AllocBytes, FreeBytes     int64
	AllocObjects, FreeObjects int64
	Stack0                    [32]uintptr
}

func (r *MemProfileRecord) InUseBytes() int64 {
	return r.AllocBytes - r.FreeBytes
}

func (r *MemProfileRecord) InUseObjects() int64 {
	return r.AllocObjects - r.FreeObjects
}

func (r *MemProfileRecord) Stack() []uintptr {
	for i, pc := range r.Stack0 {
		if pc == 0 {
			return r.Stack0[:i]
		}
	}
	return r.Stack0[:]
}

// memProfileBuckets are the object sizes the profile aggregates by.
var memProfileBuckets = [...]uintptr{
	16,
	32,
	64,
	128,
	256,
	512,
	1024,
	2048,
	4096,
	8192,
	16384,
	32768,
	65536,
	131072,
	262144,
	524288,
	1048576,
	2097152,
	4194304,
	8388608,
	16777216,
	33554432,
	67108864,
	134217728,
	268435456,
	536870912,
	1073741824,
}

// Keep the bucket scan out of allocation call sites. Inlining it duplicates
// the scan and atomic update in every small allocator wrapper, which makes
// otherwise unrelated runtime backend changes perturb native binary size.
//
//go:noinline
func recordMemProfileAlloc(size uintptr) {
	if size == 0 {
		return
	}
	size = memProfileSizeClass(size)
	for i, b := range memProfileBuckets {
		if b == size {
			memProfileAddObject(i)
			return
		}
	}
}

func memProfileSizeClass(size uintptr) uintptr {
	if size <= 16 {
		return 16
	}
	for _, b := range memProfileBuckets {
		if size <= b {
			return b
		}
	}
	return memProfileBuckets[len(memProfileBuckets)-1]
}

func MemProfile(p []MemProfileRecord, inuseZero bool) (n int, ok bool) {
	for i := range memProfileBuckets {
		if memProfileLoadObjects(i) != 0 {
			n++
		}
	}
	if len(p) < n {
		return n, false
	}
	j := 0
	for i, b := range memProfileBuckets {
		objects := memProfileLoadObjects(i)
		if objects == 0 {
			continue
		}
		p[j] = MemProfileRecord{
			ObjectSize:   int64(b),
			AllocBytes:   int64(uint64(b) * uint64(objects)),
			AllocObjects: int64(objects),
		}
		j++
	}
	return n, true
}
