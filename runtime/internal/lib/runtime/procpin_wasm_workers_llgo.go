//go:build llgo && js && wasm && llgo.wasm.workers

package runtime

import (
	_ "unsafe"

	llruntime "github.com/xgo-dev/llgo/runtime/internal/runtime"
)

//go:linkname sync_runtime_procPin sync.runtime_procPin
func sync_runtime_procPin() int {
	return llruntime.SchedulerProcID()
}

//go:linkname sync_runtime_procUnpin sync.runtime_procUnpin
func sync_runtime_procUnpin() {}
