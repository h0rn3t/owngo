//go:build owngo && owngomn && (linux || darwin)

package runtime

import (
	"unsafe"

	c "github.com/xgo-dev/llgo/runtime/internal/clite"
	psync "github.com/xgo-dev/llgo/runtime/internal/sync"
	"github.com/xgo-dev/llgo/runtime/internal/sync/atomic"
	"github.com/xgo-dev/llgo/runtime/internal/thread"
)

// Планувальник M:N (P-NEW-09.6). G виконується на власному стеку з резерву
// (mn_owngo.go) на одному з потоків M; M виконує G, лише тримаючи логічний
// процесор P, а P — не більше GOMAXPROCS. Кожна M має g0 — свій рідний
// стек, на якому крутиться цикл mnSchedule; G повертається туди через
// mnMcall і віддає M, коли паркується, поступається чи виходить.
//
// G на рідному стеку потоку (головна, ліниво створена getg на чужому
// потоці) і G після LockOSThread прив'язані до своєї M: коли така G
// паркується, її M віддає P і спить, а M, що знайде G готовою, передає їй
// свій P (mnStartLockedm).
//
// G, що блокується засобами ОС (psync.BlockEnter: pthread_cond_wait,
// блокувальні виклики рантайму), віддає P іншій M і після пробудження бере
// вільний P або виконується без P до наступної точки планування.
//
// Мігрує G лише в точках планування; перемикання проходить через атомарні
// операції черг і статусу, тож неатомарні лічильники обліку owngo лишаються
// коректними.

//go:linkname mnWait C.owngo_mn_wait
func mnWait(addr *uint32, val uint32, ns int64) c.Int

//go:linkname mnWake C.owngo_mn_wake
func mnWake(addr *uint32, all c.Int)

//go:linkname mnProcs C.owngo_mn_procs
func mnProcs() c.Int

// runtimeContextPlatform — стан G, який M:N тримає поза спільним g.
type runtimeContextPlatform struct {
	mn         *mnCtx       // стек G; nil — G на рідному стеку свого потоку
	m          m            // M потоку для G на рідному стеку
	lockedm    *m           // M, до якої G прив'язана
	lockCount  int32        // вкладені LockOSThread
	schedlink  *g           // глобальна черга
	glsContext LocalContext // змінні GLS цієї G
	pless      bool         // користувацька G виконується без P
	systemG    bool
	parking    uint32 // 1 — G на стеку з резерву в SchedulerWaiter.Park
	syscalls   int32  // вкладені марковані виклики C (EnterSyscall)

	foreignThreadAttached bool
}

// mOS — стан M планувальника.
type mOS struct {
	g0        *g     // G циклу планувальника на рідному стеку M
	g0ctx     *mnCtx // контекст рідного стека
	note      mnNote // сон M: прокидається з nextp
	nextp     *p
	lockedg   *g
	schedlink *m
	localCtx  LocalContext // TLS-змінні з блоками пакета: свої в кожної M
	call      int32        // що G просить від g0 у mnMcall
	tick      uint32       // пошуки роботи; кожен 61-й опитує poller
}

const (
	mnCallPark = 1 + iota
	mnCallGosched
	mnCallExit
)

func runtimeContextAllocSize() uintptr {
	return unsafe.Sizeof(runtimeContext{})
}

// localContextMigrates: G може вийти на іншій M, ніж почалась, тож вкладений
// вихід обгортки горутини бачить контекст іншої M.
const localContextMigrates = true

// ---- замок і нотатка -------------------------------------------------------

// mnLock — м'ютекс на futex: 0 — вільний, 1 — зайнятий, 2 — є очікувачі.
type mnLock struct{ key uint32 }

func (l *mnLock) lock() {
	if _, ok := atomic.CompareAndExchange(&l.key, 0, 1); ok {
		return
	}
	for i := 0; i < 100; i++ {
		if atomic.Load(&l.key) == 0 {
			if _, ok := atomic.CompareAndExchange(&l.key, 0, 1); ok {
				return
			}
		}
	}
	for atomic.Exchange(&l.key, 2) != 0 {
		mnWait(&l.key, 2, -1)
	}
}

func (l *mnLock) unlock() {
	if atomic.Exchange(&l.key, 0) == 2 {
		mnWake(&l.key, 0)
	}
}

// mnNote — одноразове пробудження M.
type mnNote struct{ key uint32 }

func (n *mnNote) clear() { atomic.Store(&n.key, 0) }

func (n *mnNote) wakeup() {
	atomic.Store(&n.key, 1)
	mnWake(&n.key, 1)
}

// sleep чекає wakeup не довше ns (ns < 0 — без межі); true — розбуджено.
func (n *mnNote) sleep(ns int64) bool {
	for atomic.Load(&n.key) == 0 {
		if mnWait(&n.key, 0, ns) != 0 {
			return atomic.Load(&n.key) != 0
		}
	}
	return true
}

// ---- P і глобальний стан ---------------------------------------------------

const (
	mnMaxProcs  = 256
	mnRunqSize  = 256
	mnDeadCheck = 100 * 1000 * 1000 // нс між перевірками взаємоблокування
)

// mnP — P з локальною чергою; *p і *mnP — та сама адреса.
type mnP struct {
	p       p
	lock    mnLock
	runnext *g
	runq    [mnRunqSize]*g
	head, n uint32
	link    *mnP
	idle    bool // у списку вільних
	off     bool // id >= GOMAXPROCS

	syscalltick uint32 // росте з кожним EnterSyscall на цьому P
	sysmonTick  uint32 // syscalltick, який sysmon бачив востаннє
	sysmonWhen  int64  // коли sysmon уперше побачив sysmonTick
}

func pOf(pp *p) *mnP { return (*mnP)(unsafe.Pointer(pp)) }

var mnSched struct {
	lock       mnLock
	state      uint32 // 0 — не готовий, 1 — готується, 2 — готовий
	allp       [mnMaxProcs]*mnP
	nallp      int32
	gomaxprocs int32
	pidle      *mnP
	npidle     int32
	midle      *m
	runqHead   *g
	runqTail   *g
	runqSize   int32
	nblocked   int32  // користувацькі G, що виконуються або блокуються без P
	gen        uint64 // росте з кожною готовою чи запущеною G
	deadGen    uint64 // gen на першому виявленні взаємоблокування
	deadSeen   bool
	netpoll    uint32 // 1 — lib/runtime поставив netpollHook
	polling    uint32 // 1 — якась M блокується в netpoll
	sysmon     uint32 // 1 — потік sysmon запущено
}

// timersPending ставить lib/runtime: чи є таймери, що ще спрацюють. Без
// нього взаємоблокування не виявляється.
// mnCreated — скільки M створив планувальник, крім головної. Тест P-NEW-09.9.
var mnCreated uint32

// MNCountForTesting — головна M плюс створені планувальником.
func MNCountForTesting() int { return int(atomic.Load(&mnCreated)) + 1 }

var timersPending func() bool

// SetTimersPendingHook ставить перевірку таймерів для виявлення
// взаємоблокування.
func SetTimersPendingHook(f func() bool) { timersPending = f }

// netpollHook і netpollWaiting ставить мережевий poller lib/runtime
// (P-NEW-09.7): netpollHook(delay) будить G, чиї дескриптори готові (delay < 0
// — чекати без межі, 0 — не блокуватися), а netpollWaiting каже, чи є G,
// припарковані в pollWait.
var (
	netpollHook    func(delay int64)
	netpollWaiting func() bool
)

// SetNetpollHooks ставить poller. Планувальник кличе його лише після цього
// виклику.
func SetNetpollHooks(poll func(delay int64), waiting func() bool) {
	netpollHook, netpollWaiting = poll, waiting
	atomic.Store(&mnSched.netpoll, 1)
}

// mnNetpollWaiters — чи є G, які чекають на poller.
func mnNetpollWaiters() bool {
	return atomic.Load(&mnSched.netpoll) != 0 && netpollWaiting()
}

// mnNetpollNow опитує poller без блокування, якщо ніхто не блокується в
// ньому: готові G ідуть у чергу P поточної M.
func mnNetpollNow() {
	if mnNetpollWaiters() && atomic.Load(&mnSched.polling) == 0 {
		netpollHook(0)
	}
}

func mnInit() {
	if atomic.Load(&mnSched.state) == 2 {
		return
	}
	if _, won := atomic.CompareAndExchange(&mnSched.state, 0, 1); !won {
		for atomic.Load(&mnSched.state) != 2 {
			thread.Yield()
		}
		return
	}
	n := int32(mnProcs())
	if n < 1 {
		n = 1
	}
	if n > mnMaxProcs {
		n = mnMaxProcs
	}
	psync.SetBlockHooks(mnBlockEnter, mnBlockExit)
	mnSched.lock.lock()
	mnGrowProcsLocked(n)
	mnSched.gomaxprocs = n
	mnSched.lock.unlock()
	// Головна G тримає P0, як головна горутина Go: GOMAXPROCS рахує і її.
	if gp := getg(); gp.isMain && gp.context.platform.mn == nil {
		mnSched.lock.lock()
		pp := mnPidleRemoveLocked(mnSched.allp[0])
		mnSched.lock.unlock()
		if pp != nil {
			mnAcquirep(gp.m, pp)
			mnSetPless(gp, false)
		}
	}
	atomic.Store(&mnSched.state, 2)
}

// mnGrowProcsLocked створює P до n і кладе нові у вільні.
func mnGrowProcsLocked(n int32) {
	for mnSched.nallp < n {
		pp := (*mnP)(c.Calloc(1, unsafe.Sizeof(mnP{})))
		if pp == nil {
			fatal("runtime: cannot allocate P")
		}
		pp.p.id = mnSched.nallp
		mnSched.allp[mnSched.nallp] = pp
		mnSched.nallp++
		mnPidlePutLocked(pp)
	}
}

func mnPidlePutLocked(pp *mnP) {
	if pp.idle || pp.off {
		return
	}
	pp.p.m = nil
	setpstatus(&pp.p, _Pidle)
	pp.idle = true
	pp.link = mnSched.pidle
	mnSched.pidle = pp
	mnSched.npidle++
}

func mnPidleGetLocked() *mnP {
	pp := mnSched.pidle
	if pp == nil {
		return nil
	}
	mnSched.pidle = pp.link
	pp.link = nil
	pp.idle = false
	mnSched.npidle--
	return pp
}

// mnPidleRemoveLocked забирає конкретний P зі списку вільних.
func mnPidleRemoveLocked(want *mnP) *mnP {
	for pl := &mnSched.pidle; *pl != nil; pl = &(*pl).link {
		if *pl == want {
			*pl = want.link
			want.link = nil
			want.idle = false
			mnSched.npidle--
			return want
		}
	}
	return nil
}

func mnAcquirep(mp *m, pp *mnP) {
	mp.p = &pp.p
	pp.p.m = mp
	setpstatus(&pp.p, _Prunning)
}

func mnReleasep(mp *m) *mnP {
	pp := pOf(mp.p)
	mp.p = nil
	pp.p.m = nil
	setpstatus(&pp.p, _Pidle)
	return pp
}

// mnSetPless позначає, що користувацька G виконується без P: такі G не дають
// виявити взаємоблокування, бо можуть розбудити інших.
func mnSetPless(gp *g, pless bool) {
	pf := &gp.context.platform
	if pf.pless == pless {
		return
	}
	pf.pless = pless
	if pf.systemG {
		return
	}
	if pless {
		atomic.Add(&mnSched.nblocked, 1)
	} else {
		atomic.Add(&mnSched.nblocked, -1)
	}
}

// ---- черги -----------------------------------------------------------------

func mnGlobPutLocked(gp *g) {
	gp.context.platform.schedlink = nil
	if mnSched.runqTail != nil {
		mnSched.runqTail.context.platform.schedlink = gp
	} else {
		mnSched.runqHead = gp
	}
	mnSched.runqTail = gp
	mnSched.runqSize++
}

func mnGlobGetLocked() *g {
	gp := mnSched.runqHead
	if gp == nil {
		return nil
	}
	mnSched.runqHead = gp.context.platform.schedlink
	if mnSched.runqHead == nil {
		mnSched.runqTail = nil
	}
	gp.context.platform.schedlink = nil
	mnSched.runqSize--
	return gp
}

// mnRunqPut кладе G у локальну чергу P; переповнення — половина в глобальну.
func mnRunqPut(pp *mnP, gp *g, next bool) {
	pp.lock.lock()
	if next {
		gp, pp.runnext = pp.runnext, gp
		if gp == nil {
			pp.lock.unlock()
			return
		}
	}
	if pp.n < mnRunqSize {
		pp.runq[(pp.head+pp.n)%mnRunqSize] = gp
		pp.n++
		pp.lock.unlock()
		return
	}
	var batch [mnRunqSize/2 + 1]*g
	for i := 0; i < mnRunqSize/2; i++ {
		batch[i] = pp.runq[pp.head]
		pp.head = (pp.head + 1) % mnRunqSize
		pp.n--
	}
	batch[mnRunqSize/2] = gp
	pp.lock.unlock()
	mnSched.lock.lock()
	for _, bg := range batch {
		mnGlobPutLocked(bg)
	}
	mnSched.lock.unlock()
}

func mnRunqGet(pp *mnP) *g {
	pp.lock.lock()
	defer pp.lock.unlock()
	if gp := pp.runnext; gp != nil {
		pp.runnext = nil
		return gp
	}
	if pp.n == 0 {
		return nil
	}
	gp := pp.runq[pp.head]
	pp.runq[pp.head] = nil
	pp.head = (pp.head + 1) % mnRunqSize
	pp.n--
	return gp
}

// mnRunqSteal забирає половину локальної черги victim у pp; runnext не
// краде, щоб готова G лишалася на P, що її розбудив.
func mnRunqSteal(pp, victim *mnP) *g {
	var batch [mnRunqSize / 2]*g
	victim.lock.lock()
	k := (victim.n + 1) / 2
	for i := uint32(0); i < k; i++ {
		batch[i] = victim.runq[victim.head]
		victim.runq[victim.head] = nil
		victim.head = (victim.head + 1) % mnRunqSize
	}
	victim.n -= k
	victim.lock.unlock()
	if k == 0 {
		return nil
	}
	for i := uint32(1); i < k; i++ {
		mnRunqPut(pp, batch[i], false)
	}
	return batch[0]
}

// mnAnyWork — чи є готові G у глобальній черзі чи в локальній черзі
// якогось P.
func mnAnyWork() bool {
	if atomic.Load(&mnSched.runqSize) != 0 {
		return true
	}
	for i, n := int32(0), atomic.Load(&mnSched.nallp); i < n; i++ {
		pp := mnSched.allp[i]
		pp.lock.lock()
		busy := pp.n != 0 || pp.runnext != nil
		pp.lock.unlock()
		if busy {
			return true
		}
	}
	return false
}

func mnFindRunnable(mp *m, pp *mnP) *g {
	// Без sysmon зайняті M мусять самі час від часу забирати готові
	// дескриптори, інакше мережа голодуватиме за CPU-завантажених G.
	if mp.os.tick++; mp.os.tick%61 == 0 {
		mnNetpollNow()
	}
	if gp := mnRunqGet(pp); gp != nil {
		return gp
	}
	if atomic.Load(&mnSched.runqSize) != 0 {
		mnSched.lock.lock()
		n := mnSched.runqSize/mnSched.gomaxprocs + 1
		if n > mnSched.runqSize {
			n = mnSched.runqSize
		}
		if n > mnRunqSize/2 {
			n = mnRunqSize / 2
		}
		gp := mnGlobGetLocked()
		for i := int32(1); i < n; i++ {
			bg := mnGlobGetLocked()
			if bg == nil {
				break
			}
			mnRunqPut(pp, bg, false)
		}
		mnSched.lock.unlock()
		if gp != nil {
			return gp
		}
	}
	nallp := atomic.Load(&mnSched.nallp)
	start := uint32(mp.id) % uint32(nallp)
	for i := uint32(0); i < uint32(nallp); i++ {
		victim := mnSched.allp[(start+i)%uint32(nallp)]
		if victim == pp {
			continue
		}
		if gp := mnRunqSteal(pp, victim); gp != nil {
			return gp
		}
	}
	mnNetpollNow()
	return mnRunqGet(pp)
}

// ---- M ---------------------------------------------------------------------

func mnStartm(pp *mnP) {
	mnSched.lock.lock()
	mp := mnSched.midle
	if mp != nil {
		mnSched.midle = mp.os.schedlink
		mp.os.schedlink = nil
	}
	mnSched.lock.unlock()
	if mp == nil {
		mnNewm(pp)
		return
	}
	mp.os.nextp = &pp.p
	mp.os.note.wakeup()
}

// mnWakep запускає M на вільному P, якщо такий є.
func mnWakep() {
	if atomic.Load(&mnSched.npidle) == 0 {
		return
	}
	mnSched.lock.lock()
	pp := mnPidleGetLocked()
	mnSched.lock.unlock()
	if pp != nil {
		mnStartm(pp)
	}
}

// mnHandoffp віддає P: іншій M, якщо є робота, інакше — у вільні.
func mnHandoffp(pp *mnP) {
	if mnAnyWork() {
		mnStartm(pp)
		return
	}
	mnSched.lock.lock()
	mnPidlePutLocked(pp)
	mnSched.lock.unlock()
	// Робота могла з'явитися, поки P ще не був вільним.
	if mnAnyWork() {
		mnWakep()
	}
}

// mnAllocm створює M з її g0; потік запускає викликач.
func mnAllocm() *m {
	mp := (*m)(c.Calloc(1, unsafe.Sizeof(m{})))
	ctx := allocRuntimeContext()
	if mp == nil || ctx == nil {
		fatal("runtime: cannot allocate M")
	}
	g0 := &ctx.g
	g0.context = ctx
	g0.atomicstatus = _Grunning
	g0.m = mp
	ctx.platform.systemG = true
	mp.id = nextMid(mp)
	mp.os.g0 = g0
	return mp
}

func mnNewm(pp *mnP) {
	atomic.Add(&mnCreated, 1)
	mp := mnAllocm()
	mp.os.nextp = &pp.p
	if errno := thread.CreateDetached(0, thread.RoutineFunc(wasiGCThreadStart), c.Pointer(unsafe.Pointer(mp))); errno != 0 {
		fatal("runtime: failed to create new OS thread")
	}
}

// mstart — вхід потоку M: g0 на рідному стеку потоку крутить mnSchedule.
func mstart(arg unsafe.Pointer) unsafe.Pointer {
	mp := (*m)(arg)
	setg(mp.os.g0)
	mp.os.g0ctx = mnThreadCtx()
	currentLocalContext = uintptr(unsafe.Pointer(&mp.os.localCtx))
	mnAcquirep(mp, pOf(mp.os.nextp))
	mp.os.nextp = nil
	mnSchedule(mp)
	return nil
}

// mnSleep присипляє M до wakeup. Поки спить, раз на mnDeadCheck перевіряє
// взаємоблокування: двічі поспіль те саме gen, усі P вільні, немає G без P,
// готових G і таймерів — fatal error, як у Go.
func mnSleep(mp *m) {
	for !mp.os.note.sleep(mnDeadCheck) {
		mnCheckDead()
	}
}

func mnCheckDead() {
	mnSched.lock.lock()
	defer mnSched.lock.unlock()
	count, mainExited := gState()
	dead := mnSched.npidle == mnSched.gomaxprocs && atomic.Load(&mnSched.nblocked) == 0 &&
		count > 0 && !mainExited && timersPending != nil && !mnAnyWork() && !timersPending() &&
		!mnNetpollWaiters()
	gen := atomic.Load(&mnSched.gen)
	if !dead {
		mnSched.deadSeen = false
		return
	}
	if !mnSched.deadSeen || mnSched.deadGen != gen {
		mnSched.deadSeen, mnSched.deadGen = true, gen
		return
	}
	fatal("all goroutines are asleep - deadlock!")
	c.Exit(2)
}

// mnStopm кладе M у вільні і присипляє до передачі P.
func mnStopm(mp *m) {
	mp.os.note.clear()
	mnSched.lock.lock()
	mp.os.schedlink = mnSched.midle
	mnSched.midle = mp
	mnSched.lock.unlock()
	mnSleep(mp)
	mnAcquirep(mp, pOf(mp.os.nextp))
	mp.os.nextp = nil
}

// mnStopLockedm — M прив'язаної G чекає, доки G знову готова і їй передадуть
// P. Нотатку очищено до того, як G стала видимою іншим.
func mnStopLockedm(mp *m) {
	if mp.p != nil {
		mnHandoffp(mnReleasep(mp))
	}
	mnSleep(mp)
	mnAcquirep(mp, pOf(mp.os.nextp))
	mp.os.nextp = nil
}

// mnStartLockedm передає P M, до якої прив'язана готова gp, і зупиняє mp.
func mnStartLockedm(mp *m, gp *g) {
	lm := gp.context.platform.lockedm
	lm.os.nextp = &mnReleasep(mp).p
	lm.os.note.wakeup()
	mnStopm(mp)
}

// ---- цикл планувальника ----------------------------------------------------

func mnSchedule(mp *m) {
	for {
		if mp.p == nil {
			mnStopm(mp)
			continue
		}
		pp := pOf(mp.p)
		if pp.off {
			mnDropOffP(mp)
			continue
		}
		gp := mnFindRunnable(mp, pp)
		if gp == nil {
			mnIdle(mp)
			continue
		}
		if lm := gp.context.platform.lockedm; lm != nil && lm != mp {
			mnStartLockedm(mp, gp)
			continue
		}
		for gp != nil {
			if mp.p == nil {
				// G без P (після блокування в ОС) готова знову, а P немає: у
				// глобальну чергу, M — спати.
				mnSched.lock.lock()
				mnGlobPutLocked(gp)
				mnSched.lock.unlock()
				mnWakep()
				break
			}
			gp = mnExecute(mp, gp)
		}
	}
}

func mnIdle(mp *m) {
	pp := mnReleasep(mp)
	mnSched.lock.lock()
	mnPidlePutLocked(pp)
	mnSched.lock.unlock()
	if mnAnyWork() {
		mnSched.lock.lock()
		pp = mnPidleGetLocked()
		mnSched.lock.unlock()
		if pp != nil {
			mnAcquirep(mp, pp)
			return
		}
	}
	// M без роботи блокується в poller, якщо там ще ніхто не чекає.
	// Розбуджені G без P ідуть у глобальну чергу, а mnReady запускає для
	// них M на вільному P.
	if mnNetpollWaiters() {
		if _, won := atomic.CompareAndExchange(&mnSched.polling, 0, 1); won {
			netpollHook(-1)
			atomic.Store(&mnSched.polling, 0)
			// M, що віддали P, поки ця чекала, побачили polling і заснули.
			// Вільний P після скидання polling означає, що poller знову
			// нікому стерегти: ця M бере P і шукає роботу, а без неї
			// повертається в poller. Без вільного P у poller прийде M, що
			// тримає P, коли та залишиться без роботи.
			if atomic.Load(&mnSched.npidle) != 0 {
				mnSched.lock.lock()
				pp = mnPidleGetLocked()
				mnSched.lock.unlock()
				if pp != nil {
					mnAcquirep(mp, pp)
				}
			}
			return
		}
	}
	mnStopm(mp)
}

// mnDropOffP — P поза GOMAXPROCS: його чергу — в глобальну, M — спати.
func mnDropOffP(mp *m) {
	pp := mnReleasep(mp)
	for gp := mnRunqGet(pp); gp != nil; gp = mnRunqGet(pp) {
		mnSched.lock.lock()
		mnGlobPutLocked(gp)
		mnSched.lock.unlock()
	}
	mnWakep()
	mnStopm(mp)
}

// mnExecute виконує gp на M до її повернення в g0 і повертає G, яку M має
// виконати одразу (прив'язану, якій повернули P), або nil.
func mnExecute(mp *m, gp *g) *g {
	casgstatus(gp, _Grunnable, _Grunning)
	gp.m = mp
	mp.curg = gp
	atomic.Add(&mnSched.gen, 1)
	// Прямий запис слота TLS замість setg: attachTraceback на кожному
	// перемиканні робив би системні виклики (pthread_getattr_np, gettid,
	// sigmask) і прив'язував би вузол трасування G до стека потоку.
	currentG = uintptr(unsafe.Pointer(gp))
	mnSwitch(mp.os.g0ctx, gp.context.platform.mn)
	currentG = uintptr(unsafe.Pointer(mp.os.g0))
	mp.curg = nil
	call := mp.os.call
	mp.os.call = 0
	locked := gp.context.platform.lockedm == mp
	if locked {
		mp.os.note.clear()
	}
	switch call {
	case mnCallPark:
		// Після Gwaiting Ready з іншої M може вже виконати G, а та звільнить
		// своє очікування, тож ні його, ні gp тут більше не читаємо.
		// Ready, що прийшов раніше, чекає на Gwaiting, поки стоїть parking.
		mnSetPless(gp, false)
		casgstatus(gp, _Grunning, _Gwaiting)
	case mnCallGosched:
		casgstatus(gp, _Grunning, _Grunnable)
		mnSetPless(gp, false)
		mnSched.lock.lock()
		mnGlobPutLocked(gp)
		mnSched.lock.unlock()
		mnWakep()
	case mnCallExit:
		mnGoexit0(mp, gp)
		return nil
	default:
		fatal("runtime: goroutine returned to the scheduler without a request")
	}
	if !locked {
		return nil
	}
	mnStopLockedm(mp)
	return gp
}

// mnMcall повертає G у g0 її M з проханням call.
func mnMcall(gp *g, call int32) {
	mp := gp.m
	mp.os.call = call
	mnSwitch(gp.context.platform.mn, mp.os.g0ctx)
}

// ---- G ---------------------------------------------------------------------

func initRuntimeContext(ctx *runtimeContext, callergp *g, status uint32) *g {
	gp := initG(ctx, callergp, status)
	if status == _Grunning {
		// Ліниво створена G на рідному стеку потоку (головна чи чужий потік):
		// своя M без g0, G прив'язана до неї і поки виконується без P.
		mp := &ctx.platform.m
		mp.id = nextMid(mp)
		mp.curg = gp
		gp.m = mp
		ctx.platform.lockedm = mp
		mnSetPless(gp, true)
	}
	return gp
}

func newprocBackend(fn goroutineFunc, arg unsafe.Pointer, stackSize uintptr, callergp *g) {
	mnInit()
	gp := newproc1(fn, arg, callergp)
	ctx := mnCtxNew(stackSize, goroutineFunc(mnGEntry), unsafe.Pointer(gp), gp.goid)
	if ctx == nil {
		releaseG()
		releaseStartArg(gp)
		freeRuntimeContext(gp.context)
		panic("runtime: failed to allocate goroutine stack")
	}
	gp.context.platform.mn = ctx
	mnReady(gp, false)
}

// mnReady робить готову gp видимою: у локальну чергу P поточної M (next —
// у runnext, щоб розбуджена G йшла наступною) або в глобальну.
func mnReady(gp *g, next bool) {
	atomic.Add(&mnSched.gen, 1)
	if cur := getg(); cur.m != nil && cur.m.p != nil {
		mnRunqPut(pOf(cur.m.p), gp, next)
	} else {
		mnSched.lock.lock()
		mnGlobPutLocked(gp)
		mnSched.lock.unlock()
	}
	mnWakep()
}

func mnGEntry(arg unsafe.Pointer) unsafe.Pointer {
	gp := (*g)(arg)
	fn, startarg := gp.startfn, gp.startarg
	gp.startfn = nil
	fn(startarg)
	if gp.isMain {
		// Повернення з main завершує процес з тієї M, де воно сталося, як у Go;
		// хвіст точки входу після RunWasmMain не виконується.
		ReportExit()
		c.Exit(0)
	}
	mnMcall(gp, mnCallExit)
	return nil
}

//go:linkname mnMainTask C.__llgo_wasm_main
func mnMainTask(unsafe.Pointer) unsafe.Pointer

// RunWasmMain запускає ініціалізацію пакетів і main.main (задача
// __llgo_wasm_main від компілятора) як звичайну G на стеку з резерву, а
// головний потік робить M0: його рідний стек стає g0 планувальника. Ім'я —
// спільне з бекендами wasm, які кличе та сама точка входу.
func RunWasmMain() {
	boot := getg()
	if !boot.isMain || boot.context.platform.mn != nil {
		fatal("runtime: invalid main goroutine")
	}
	// Початкова G створена ліниво й належить деструктору TLS: setg з тією
	// самою G передає її рантайму, інакше перший setg іншої G її звільнить.
	setg(boot)
	mnInit()
	mp := boot.m
	mainG := newproc1(goroutineFunc(mnMainTask), nil, boot)
	mainG.isMain = true
	ctx := mnCtxNew(0, goroutineFunc(mnGEntry), unsafe.Pointer(mainG), mainG.goid)
	if ctx == nil {
		fatal("runtime: failed to allocate main goroutine stack")
	}
	mainG.context.platform.mn = ctx
	// Початкова G головного потоку стає g0 M0: не користувацька G і не
	// прив'язана.
	boot.isMain = false
	pf := &boot.context.platform
	mnSetPless(boot, false)
	pf.systemG = true
	pf.lockedm = nil
	releaseG()
	mp.os.g0 = boot
	mp.os.g0ctx = mnThreadCtx()
	if mp.p == nil {
		mnStopm(mp)
	}
	mnRunqPut(pOf(mp.p), mainG, false)
	mnSchedule(mp)
}

// mnGoexit0 на g0 звільняє G, що вийшла: стек більше не виконується.
func mnGoexit0(mp *m, gp *g) {
	releaseStartArg(gp)
	mnSetPless(gp, false)
	if !gp.context.platform.systemG {
		releaseGAndCheckDeadlock()
	}
	casgstatus(gp, _Grunning, _Gdead)
	if gp.context.platform.lockedm == mp {
		gp.context.platform.lockedm = nil
		mp.os.lockedg = nil
	}
	releaseGoroutineLocalBlocks(&gp.context.platform.glsContext)
	mnCtxFree(gp.context.platform.mn)
	freeRuntimeContext(gp.context)
}

func goexitBackend(gp *g) {
	if gp.context.platform.mn != nil {
		mnMcall(gp, mnCallExit)
		fatal("runtime: exited goroutine resumed")
	}
	leaveCurrentLocalContext()
	releaseGoroutineLocalBlocks(&gp.context.platform.glsContext)
	mp := gp.m
	if mp.p != nil {
		mnHandoffp(mnReleasep(mp))
	}
	mnSetPless(gp, false)
	releaseStartArg(gp)
	ownedByLifecycle := currentGUsesLifecycle()
	if !ownedByLifecycle && !gp.context.platform.systemG {
		releaseGAndCheckDeadlock()
	}
	casgstatus(gp, _Grunning, _Gdead)
	if !ownedByLifecycle {
		setg(nil)
		freeRuntimeContext(gp.context)
	}
	thread.Exit()
}

// CooperativeSafepoint — точка поступки кооперативних safepoints; M:N без
// витіснення (P-NEW-09.10) нічого не робить.
func CooperativeSafepoint() {}

func tryCasgstatus(gp *g, oldval, newval uint32) bool {
	_, ok := atomic.CompareAndExchange(&gp.atomicstatus, oldval, newval)
	return ok
}

// SchedulerWaiter — одноразове сповіщення G, що чекає: Ready раніше за Park
// не губиться.
type SchedulerWaiter struct {
	gp       *g
	notified uint32
}

func CurrentSchedulerWaiter() SchedulerWaiter {
	return SchedulerWaiter{gp: getg()}
}

func (w *SchedulerWaiter) Park() {
	gp := w.gp
	if gp == nil || getg() != gp {
		fatal("runtime: invalid scheduler waiter")
		return
	}
	pf := &gp.context.platform
	if pf.syscalls > 0 {
		// Callback з C через покажчик на функцію без //export: G спершу
		// виходить з маркованого виклику, як у CallbackEnter.
		tok := CallbackEnter()
		w.Park()
		CallbackExit(tok)
		return
	}
	if pf.mn != nil {
		// parking ставиться раніше за перевірку notified, а Ready пише
		// notified раніше за читання parking: хоч один побачить іншого.
		atomic.Store(&pf.parking, 1)
	}
	if _, ok := atomic.CompareAndExchange(&w.notified, 1, 0); ok {
		atomic.Store(&pf.parking, 0)
		return
	}
	mnInit()
	if pf.mn != nil {
		mnMcall(gp, mnCallPark)
		atomic.Store(&pf.parking, 0)
		atomic.Store(&w.notified, 0)
		return
	}
	// G на рідному стеку: її M віддає P і спить, доки G не стане готовою.
	mp := gp.m
	mp.os.note.clear()
	casgstatus(gp, _Grunning, _Gwaiting)
	mnSetPless(gp, false)
	if atomic.Load(&w.notified) == 1 && tryCasgstatus(gp, _Gwaiting, _Grunnable) {
		casgstatus(gp, _Grunnable, _Grunning)
		if mp.p == nil {
			mnSetPless(gp, true)
		}
		atomic.Store(&w.notified, 0)
		return
	}
	mnStopLockedm(mp)
	casgstatus(gp, _Grunnable, _Grunning)
	atomic.Store(&w.notified, 0)
}

func (w *SchedulerWaiter) Ready() {
	gp := w.gp
	if gp == nil {
		fatal("runtime: ready of invalid scheduler waiter")
		return
	}
	if _, ok := atomic.CompareAndExchange(&w.notified, 0, 1); !ok {
		return
	}
	// G, що вже паркується, ще не побачила notified або g0 її M ще не
	// опублікувала Gwaiting: чекаємо, доки G стане Gwaiting або сама
	// забере сповіщення.
	for !tryCasgstatus(gp, _Gwaiting, _Grunnable) {
		if atomic.Load(&gp.context.platform.parking) == 0 {
			return
		}
		thread.Yield()
	}
	mnReady(gp, true)
}

// SchedulerProcID — номер P поточної M; без P — 0.
func SchedulerProcID() int {
	if gp := getg(); gp.m != nil && gp.m.p != nil {
		return int(gp.m.p.id)
	}
	return 0
}

func goschedBackend() {
	mnInit()
	gp := getg()
	if gp.context.platform.syscalls > 0 {
		tok := CallbackEnter()
		goschedBackend()
		CallbackExit(tok)
		return
	}
	if gp.context.platform.mn != nil {
		mnMcall(gp, mnCallGosched)
		return
	}
	mp := gp.m
	if mp.p == nil {
		thread.Yield()
		return
	}
	// G на рідному стеку: у глобальну чергу і чекати свого P, як Park.
	mp.os.note.clear()
	casgstatus(gp, _Grunning, _Grunnable)
	mnSched.lock.lock()
	mnGlobPutLocked(gp)
	mnSched.lock.unlock()
	mnStopLockedm(mp)
	casgstatus(gp, _Grunnable, _Grunning)
}

// ---- блокування засобами ОС ------------------------------------------------

func mnBlockEnter() {
	gp := (*g)(unsafe.Pointer(currentG))
	if gp == nil || gp.m == nil || gp == gp.m.os.g0 || gp.m.p == nil {
		return
	}
	mp := gp.m
	mnSetPless(gp, true)
	mnHandoffp(mnReleasep(mp))
}

func mnBlockExit() {
	gp := (*g)(unsafe.Pointer(currentG))
	if gp == nil || gp.m == nil || gp == gp.m.os.g0 || gp.m.p != nil || !gp.context.platform.pless {
		return
	}
	if atomic.Load(&mnSched.npidle) == 0 {
		return // виконується без P до наступної точки планування
	}
	mnSched.lock.lock()
	pp := mnPidleGetLocked()
	mnSched.lock.unlock()
	if pp != nil {
		mnAcquirep(gp.m, pp)
		mnSetPless(gp, false)
	}
}

// ---- GOMAXPROCS і LockOSThread ---------------------------------------------

// SchedGOMAXPROCS повертає попереднє GOMAXPROCS і, якщо n > 0, ставить нове.
func SchedGOMAXPROCS(n int) int {
	mnInit()
	mnSched.lock.lock()
	old := mnSched.gomaxprocs
	if n <= 0 || int32(n) == old {
		mnSched.lock.unlock()
		return int(old)
	}
	np := int32(n)
	if np > mnMaxProcs {
		np = mnMaxProcs
	}
	mnSched.gomaxprocs = np
	for i := int32(0); i < mnSched.nallp; i++ {
		pp := mnSched.allp[i]
		off := i >= np
		if off == pp.off {
			continue
		}
		pp.off = off
		if off && pp.idle {
			mnPidleRemoveLocked(pp)
			pp.off = true
		}
		if !off && pp.p.m == nil {
			mnPidlePutLocked(pp)
		}
	}
	mnGrowProcsLocked(np)
	mnSched.lock.unlock()
	for i := old; i < np; i++ {
		mnWakep()
	}
	return int(old)
}

// SchedLockOSThread прив'язує поточну G до її M; G на рідному стеку вже
// прив'язана.
func SchedLockOSThread() {
	mnInit()
	gp := getg()
	pf := &gp.context.platform
	if pf.mn == nil {
		return
	}
	pf.lockCount++
	pf.lockedm = gp.m
	gp.m.os.lockedg = gp
}

func SchedUnlockOSThread() {
	gp := getg()
	pf := &gp.context.platform
	if pf.mn == nil || pf.lockCount == 0 {
		return
	}
	pf.lockCount--
	if pf.lockCount == 0 {
		gp.m.os.lockedg = nil
		pf.lockedm = nil
	}
}

// ---- решта входів бекенду ---------------------------------------------------

func MarkSystemGoroutine() {
	gp := getg()
	pf := &gp.context.platform
	if pf.systemG {
		return
	}
	if pf.pless {
		mnSetPless(gp, false)
		pf.systemG = true
		pf.pless = true
	} else {
		pf.systemG = true
	}
	releaseGAndCheckDeadlock()
}

func MarkUserGoroutine() {
	gp := getg()
	pf := &gp.context.platform
	if !pf.systemG {
		return
	}
	retainG()
	pless := pf.pless
	pf.systemG, pf.pless = false, false
	mnSetPless(gp, pless)
}

func SchedulerMultiplexesGoroutinesForTesting() bool { return true }

func GMPForTesting() (goid, parentGoid uint64, mid int64, pid int32, gstatus, pstatus uint32, linked bool) {
	gp := getg()
	if gp == nil || gp.m == nil || gp.m.p == nil {
		return
	}
	mp := gp.m
	pp := mp.p
	return gp.goid, gp.parentGoid, mp.id, pp.id, readgstatus(gp), readpstatus(pp),
		mp.curg == gp && pp.m == mp
}
