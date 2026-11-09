//go:build (llgo && wasm && go1.25 && !(wasip1 && llgo.wasi_threads)) || (owngo && owngomn && (linux || darwin) && go1.25)

package runtime

import _ "unsafe"

//go:linkname sync_runtime_SemacquireWaitGroup sync.runtime_SemacquireWaitGroup
func sync_runtime_SemacquireWaitGroup(addr *uint32, durable bool) {
	syncWaitGroupAcquire(addr, durable)
}
