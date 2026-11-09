//go:build owngo && owngomn && (linux || darwin)

package runtime

import llruntime "github.com/xgo-dev/llgo/runtime/internal/runtime"

// LockOSThread прив'язує горутину до її потоку ОС: планувальник M:N більше
// не переносить її на інший потік, а потік не виконує інших горутин.
func LockOSThread() { llruntime.SchedLockOSThread() }

func UnlockOSThread() { llruntime.SchedUnlockOSThread() }
