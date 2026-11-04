//go:build baremetal

package runtime

var memProfileObjects [len(memProfileBuckets)]uintptr

func memProfileAddObject(bucket int) {
	memProfileObjects[bucket]++
}

func memProfileLoadObjects(bucket int) uint64 {
	return uint64(memProfileObjects[bucket])
}
