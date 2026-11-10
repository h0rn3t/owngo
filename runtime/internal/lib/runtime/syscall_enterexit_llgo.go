package runtime

import llruntime "github.com/xgo-dev/llgo/runtime/internal/runtime"

// entersyscall/exitsyscall — хуки syscall.Syscall: під M:N owngo позначають
// виклик для sysmon (P-NEW-09.8), інакше нічого не роблять.
func entersyscall() { llruntime.EnterSyscall() }

func exitsyscall() { llruntime.ExitSyscall() }
