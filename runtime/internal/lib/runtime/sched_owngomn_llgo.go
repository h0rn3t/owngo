//go:build owngo && owngomn && (linux || darwin)

package runtime

import llruntime "github.com/xgo-dev/llgo/runtime/internal/runtime"

// Входи планувальника M:N (P-NEW-09.6): GOMAXPROCS змінює кількість P, а
// перевірка таймерів не дає прийняти сон за взаємоблокування.

func gomaxprocs(n int) int { return llruntime.SchedGOMAXPROCS(n) }

func init() {
	// Без замка: таймерів могло ще не бути, і timerSchedulerMu тоді не
	// ініціалізований. Довжина — одне слово, а взаємоблокування
	// підтверджується двома перевірками поспіль.
	llruntime.SetTimersPendingHook(func() bool { return len(timerSchedulerHeap) > 0 })
}
