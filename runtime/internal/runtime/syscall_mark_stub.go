//go:build !llgo

package runtime

// Маркери викликів C (P-NEW-09.8) для хостових тестів компілятора.

func EnterSyscall()            {}
func ExitSyscall()             {}
func CallbackEnter() uintptr   { return 0 }
func CallbackExit(tok uintptr) {}
