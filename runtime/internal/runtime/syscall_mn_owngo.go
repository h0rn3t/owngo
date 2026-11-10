//go:build owngo && owngomn && (linux || darwin)

package runtime

import (
	"unsafe"

	c "github.com/xgo-dev/llgo/runtime/internal/clite"
	"github.com/xgo-dev/llgo/runtime/internal/sync/atomic"
	"github.com/xgo-dev/llgo/runtime/internal/thread"
)

// Блокувальні виклики C під M:N (P-NEW-09.8), як entersyscall/exitsyscall і
// sysmon у Go. Компілятор ставить EnterSyscall і ExitSyscall навколо виклику
// функції C, а syscall.Syscall кличе їх через runtime.entersyscall.
//
// EnterSyscall не віддає P: лише ставить йому статус _Psyscall. M лишає P у
// mp.p, але не володіє ним, доки ExitSyscall не поверне статус _Prunning
// атомарним CAS. Якщо виклик триває, sysmon сам забирає P тим самим CAS і
// віддає його іншій M. Тоді ExitSyscall бере вільний P або G виконується без
// P до наступної точки планування, як після psync.BlockExit.
//
// Хто забрав P з _Psyscall не в його M (sysmon або M, що повертається з
// власного виклику на той самий P), той і рахує G у виклику в nblocked:
// вона виконується без P і може розбудити інших.

const _Psyscall = 2

const (
	sysmonMinDelay = 20 * 1000        // 20 мкс: найкоротший виклик, за яким sysmon забирає P
	sysmonMaxDelay = 10 * 1000 * 1000 // 10 мс
	sysmonLongCall = 10 * 1000 * 1000 // P забирається навіть без роботи для нього
)

//go:linkname mnNanotime C.owngo_mn_nanotime
func mnNanotime() int64

//go:linkname mnErrno C.owngo_mn_errno
func mnErrno() c.Int

//go:linkname mnSetErrno C.owngo_mn_set_errno
func mnSetErrno(c.Int)

// EnterSyscall позначає, що G входить у виклик C, який може заблокуватися.
func EnterSyscall() {
	gp := (*g)(unsafe.Pointer(currentG))
	if gp == nil {
		return
	}
	pf := &gp.context.platform
	if pf.syscalls > 0 {
		pf.syscalls++
		return
	}
	mp := gp.m
	if mp == nil || gp == mp.os.g0 {
		return
	}
	pf.syscalls = 1
	// G без P теж рахує глибину: callback з цього виклику мусить
	// прив'язати її до M.
	if mp.p == nil {
		return
	}
	if atomic.Load(&mnSched.sysmon) == 0 {
		mnStartSysmon()
	}
	pp := pOf(mp.p)
	atomic.Add(&pp.syscalltick, 1)
	setpstatus(&pp.p, _Psyscall)
}

// ExitSyscall повертає G з виклику C: забирає свій P назад, а якщо його
// забрали, — вільний P. errno виклику зберігається.
func ExitSyscall() {
	gp := (*g)(unsafe.Pointer(currentG))
	if gp == nil {
		return
	}
	pf := &gp.context.platform
	if pf.syscalls == 0 {
		return
	}
	if pf.syscalls--; pf.syscalls > 0 {
		return
	}
	mp := gp.m
	if mp.p == nil {
		return // увійшла без P
	}
	pp := pOf(mp.p)
	if _, ok := atomic.CompareAndExchange(&pp.p.status, _Psyscall, _Prunning); ok {
		if victim := pp.p.m; victim != mp {
			// P тим часом пішов іншій M, і вона теж у виклику C: тепер без P
			// вона.
			atomic.Add(&mnSched.nblocked, 1)
			pp.p.m = mp
		}
		return
	}
	errno := mnErrno()
	mp.p = nil
	// Той, хто забрав P, уже порахував цю G в nblocked.
	if pf.systemG {
		atomic.Add(&mnSched.nblocked, -1)
	} else {
		pf.pless = true
	}
	if atomic.Load(&mnSched.npidle) != 0 {
		mnSched.lock.lock()
		pp = mnPidleGetLocked()
		mnSched.lock.unlock()
		if pp != nil {
			mnAcquirep(mp, pp)
			mnSetPless(gp, false)
		}
	}
	mnSetErrno(errno)
}

// CallbackEnter — вхід у функцію //export. Коли C, у яку G увійшла
// маркованим викликом, кличе Go, G повертає собі P і прив'язується до M до
// повернення в C: кадри C на її стеку чекають на той самий потік. Результат
// передається в CallbackExit.
func CallbackEnter() uintptr {
	gp := (*g)(unsafe.Pointer(currentG))
	if gp == nil {
		return 0
	}
	pf := &gp.context.platform
	depth := pf.syscalls
	if depth == 0 {
		return 0
	}
	pf.syscalls = 1
	ExitSyscall()
	SchedLockOSThread()
	return uintptr(depth)
}

// CallbackExit — повернення з функції //export у C.
func CallbackExit(tok uintptr) {
	if tok == 0 {
		return
	}
	SchedUnlockOSThread()
	EnterSyscall()
	getg().context.platform.syscalls = int32(tok)
}

func mnStartSysmon() {
	if _, won := atomic.CompareAndExchange(&mnSched.sysmon, 0, 1); !won {
		return
	}
	mp := mnAllocm()
	if errno := thread.CreateDetached(0, thread.RoutineFunc(mnSysmon), c.Pointer(unsafe.Pointer(mp))); errno != 0 {
		fatal("runtime: failed to create sysmon thread")
	}
}

// mnSysmon — потік sysmon без P: раз на 20 мкс – 10 мс забирає P у M, що
// задовго в маркованому виклику C. Без роботи для забраних P пауза
// подвоюється після 50 оглядів, як у Go.
func mnSysmon(arg unsafe.Pointer) unsafe.Pointer {
	mp := (*m)(arg)
	setg(mp.os.g0)
	currentLocalContext = uintptr(unsafe.Pointer(&mp.os.localCtx))
	delay, idle := int64(sysmonMinDelay), 0
	for {
		mp.os.note.sleep(delay)
		if mnRetake(mnNanotime()) {
			delay, idle = sysmonMinDelay, 0
			continue
		}
		if idle++; idle > 50 && delay < sysmonMaxDelay {
			delay *= 2
		}
	}
}

// mnRetake забирає P, що стоять у _Psyscall з тим самим syscalltick з
// минулого огляду, якщо для них є робота, вільних P немає або виклик
// триває довше за sysmonLongCall.
func mnRetake(now int64) bool {
	mnSched.lock.lock()
	n := mnSched.nallp
	mnSched.lock.unlock()
	retook := false
	for i := int32(0); i < n; i++ {
		pp := mnSched.allp[i]
		if atomic.Load(&pp.p.status) != _Psyscall {
			continue
		}
		if t := atomic.Load(&pp.syscalltick); t != pp.sysmonTick {
			pp.sysmonTick, pp.sysmonWhen = t, now
			continue
		}
		pp.lock.lock()
		empty := pp.n == 0 && pp.runnext == nil
		pp.lock.unlock()
		if empty && atomic.Load(&mnSched.npidle) > 0 && now-pp.sysmonWhen < sysmonLongCall {
			continue
		}
		if _, ok := atomic.CompareAndExchange(&pp.p.status, _Psyscall, _Pidle); !ok {
			continue
		}
		pp.p.m = nil
		atomic.Add(&mnSched.nblocked, 1)
		mnHandoffp(pp)
		retook = true
	}
	return retook
}
