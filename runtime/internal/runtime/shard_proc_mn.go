//go:build owngo && owngomn && (linux || darwin)

package runtime

import "unsafe"

// procShard — індекс шарду обліку цього P. Без P (потік у C чи sysmon)
// шард 0: новий шард на кожну M не з'являється, лічильники лишаються
// атомарними. true — шард уже обрано, TLS потоку не чіпаємо.
func procShard() (uint32, bool) {
	gp := (*g)(unsafe.Pointer(currentG))
	if gp == nil || gp.m == nil || gp.m.p == nil {
		return 0, true
	}
	return uint32(gp.m.p.id) % accountShardCount, true
}

// ProcIDForTesting — id поточного P або -1, якщо P немає.
func ProcIDForTesting() int32 {
	gp := (*g)(unsafe.Pointer(currentG))
	if gp == nil || gp.m == nil || gp.m.p == nil {
		return -1
	}
	return gp.m.p.id
}
