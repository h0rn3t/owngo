// Контексти і стеки G для M:N: теги owngo і owngomn (P-NEW-09.5).
//
// Контекст — збережений sp. owngo_mn_swap кладе на поточний стек регістри,
// які зберігає викликаний (і стан FPU на amd64), пише sp у from і бере sp
// іншого контексту; повернення з неї — вже в іншому контексті. Новий
// контекст має на вершині стека підготовлений кадр swap, чий адрес
// повернення — owngo_mn_start. Той кличе owngo_mn_begin, а потім переходить у
// fn(arg) контексту з нульовою адресою повернення: обхід кадрів зупиняється
// на fn (для G — mnGEntry, у трасуванні runtime.goexit), як у gc.
//
// Стек G — резерв mmap: знизу охорона OWNGO_MN_GUARD без доступу, вище —
// стек, на самій вершині — заголовок owngo_mn_ctx. Фізичні сторінки ОС
// дає при доступі, стек ніколи не переміщується. Звільнені стеки лягають у
// пул; перші MN_POOL_HOT лишаються зі сторінками, решта віддає їх ОС
// (madvise), а резерв лишає.
//
// Вихід за стек — доступ до охорони. Обробник SIGSEGV/SIGBUS працює на
// sigaltstack потоку (стек G вичерпано), друкує fatal error і завершує
// процес кодом 2; інші збої передає попередньому обробнику (fault.c).
//
// ASan: перемикання анотується __sanitizer_start_switch_fiber і
// __sanitizer_finish_switch_fiber. Символи слабкі: без рантайму ASan їх
// немає і анотацій теж.
#if defined(__linux__) && !defined(_GNU_SOURCE)
#define _GNU_SOURCE
#endif
#include <errno.h>
#include <pthread.h>
#include <signal.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <sys/mman.h>
#include <unistd.h>

#define OWNGO_MN_GUARD ((size_t)64 << 10)
#define MN_ALTSTACK ((size_t)256 << 10)
#define MN_POOL_HOT 64

typedef struct owngo_mn_ctx {
	void *sp;      // збережений sp, поки контекст не виконується
	char *lo;      // нижня межа стека (над охороною); для потоку — від ASan
	size_t size;   // розмір стека від lo
	void *fake;    // fake stack ASan, збережений при виході з контексту
	uint64_t goid; // номер G для звіту про переповнення
	char *map;     // початок резерву (охорона), NULL — стек потоку
	size_t mapsize;
	void *(*fn)(void *);
	void *arg;
	struct owngo_mn_ctx *next; // пул
} owngo_mn_ctx;

// ---- перемикання ----------------------------------------------------------

#if defined(__APPLE__)
#define MN_SYM(name) "_" #name
#define MN_FUNC(name) ".globl " MN_SYM(name) "\n.p2align 4\n" MN_SYM(name) ":\n"
#define MN_END(name)
#else
#define MN_SYM(name) #name
#define MN_FUNC(name) ".globl " #name "\n.type " #name ",@function\n.p2align 4\n" #name ":\n"
#define MN_END(name) ".size " #name ", .-" #name "\n"
#endif

void owngo_mn_swap(void **from_sp, void *to_sp);
void owngo_mn_start(void);
struct owngo_mn_ctx *owngo_mn_begin(void);

// owngo_mn_start читає fn і arg за цими зсувами.
_Static_assert(offsetof(owngo_mn_ctx, fn) == 56, "owngo_mn_ctx.fn");
_Static_assert(offsetof(owngo_mn_ctx, arg) == 64, "owngo_mn_ctx.arg");

#if defined(__x86_64__)
// Кадр swap знизу вгору: керівне слово x87 (2 Б) і MXCSR (4 Б) у 8 байтах,
// r15, r14, r13, r12, rbx, rbp, адреса повернення.
#define MN_FRAME 64
__asm__(".text\n" MN_FUNC(owngo_mn_swap)
	"pushq %rbp\n"
	"pushq %rbx\n"
	"pushq %r12\n"
	"pushq %r13\n"
	"pushq %r14\n"
	"pushq %r15\n"
	"subq $8, %rsp\n"
	"fnstcw (%rsp)\n"
	"stmxcsr 4(%rsp)\n"
	"movq %rsp, (%rdi)\n"
	"movq %rsi, %rsp\n"
	"fldcw (%rsp)\n"
	"ldmxcsr 4(%rsp)\n"
	"addq $8, %rsp\n"
	"popq %r15\n"
	"popq %r14\n"
	"popq %r13\n"
	"popq %r12\n"
	"popq %rbx\n"
	"popq %rbp\n"
	"ret\n"
	MN_END(owngo_mn_swap)
	// sp = вершина, вирівняна на 16: call і push $0 дають fn те саме
	// вирівнювання, що й звичайний call. fn не повертається.
	MN_FUNC(owngo_mn_start)
	"xorl %ebp, %ebp\n"
	"call " MN_SYM(owngo_mn_begin) "\n"
	"movq 64(%rax), %rdi\n"
	"pushq $0\n"
	"jmpq *56(%rax)\n"
	MN_END(owngo_mn_start));

static void *mn_init_frame(char *top) {
	uint64_t *f = (uint64_t *)(top - MN_FRAME);
	memset(f, 0, MN_FRAME);
	uint16_t cw;
	uint32_t csr;
	__asm__ volatile("fnstcw %0" : "=m"(cw));
	__asm__ volatile("stmxcsr %0" : "=m"(csr));
	memcpy((char *)f, &cw, sizeof cw);
	memcpy((char *)f + 4, &csr, sizeof csr);
	f[7] = (uint64_t)(uintptr_t)owngo_mn_start;
	// Після ret sp = top: вирівняний на 16, як на вході в функцію до call.
	return f;
}
#elif defined(__aarch64__)
// Кадр swap: x19–x28, x29, x30, d8–d15; 160 Б, вирівняно до 176.
#define MN_FRAME 176
__asm__(".text\n" MN_FUNC(owngo_mn_swap)
	"sub sp, sp, #176\n"
	"stp x19, x20, [sp, #0]\n"
	"stp x21, x22, [sp, #16]\n"
	"stp x23, x24, [sp, #32]\n"
	"stp x25, x26, [sp, #48]\n"
	"stp x27, x28, [sp, #64]\n"
	"stp x29, x30, [sp, #80]\n"
	"stp d8, d9, [sp, #96]\n"
	"stp d10, d11, [sp, #112]\n"
	"stp d12, d13, [sp, #128]\n"
	"stp d14, d15, [sp, #144]\n"
	"mov x2, sp\n"
	"str x2, [x0]\n"
	"mov sp, x1\n"
	"ldp x19, x20, [sp, #0]\n"
	"ldp x21, x22, [sp, #16]\n"
	"ldp x23, x24, [sp, #32]\n"
	"ldp x25, x26, [sp, #48]\n"
	"ldp x27, x28, [sp, #64]\n"
	"ldp x29, x30, [sp, #80]\n"
	"ldp d8, d9, [sp, #96]\n"
	"ldp d10, d11, [sp, #112]\n"
	"ldp d12, d13, [sp, #128]\n"
	"ldp d14, d15, [sp, #144]\n"
	"add sp, sp, #176\n"
	"ret\n"
	MN_END(owngo_mn_swap)
	// fn входить з нульовими x29 і x30; fn не повертається.
	MN_FUNC(owngo_mn_start)
	"mov x29, #0\n"
	"bl " MN_SYM(owngo_mn_begin) "\n"
	"ldr x1, [x0, #56]\n"
	"ldr x0, [x0, #64]\n"
	"mov x30, #0\n"
	"br x1\n"
	MN_END(owngo_mn_start));

static void *mn_init_frame(char *top) {
	uint64_t *f = (uint64_t *)(top - MN_FRAME);
	memset(f, 0, MN_FRAME);
	f[11] = (uint64_t)(uintptr_t)owngo_mn_start; // x30
	return f;
}
#else
#error "owngo_mn: unsupported architecture"
#endif

#if defined(__linux__)
__asm__(".section .note.GNU-stack,\"\",@progbits\n.text\n");
#endif

// ---- ASan -----------------------------------------------------------------

void __sanitizer_start_switch_fiber(void **fake_stack_save, const void *bottom, size_t size)
	__attribute__((weak));
void __sanitizer_finish_switch_fiber(void *fake_stack_save, const void **bottom_old, size_t *size_old)
	__attribute__((weak));

// ---- стан потоку ----------------------------------------------------------

static __thread owngo_mn_ctx mn_thread; // контекст власного стека потоку
static __thread owngo_mn_ctx *mn_cur;   // контекст, що виконується; NULL — потік
static __thread owngo_mn_ctx *mn_from;  // хто перемкнув на mn_cur
static __thread int mn_altstack_set;

static long mn_page;
static pthread_once_t mn_once = PTHREAD_ONCE_INIT;
static struct sigaction mn_prev_segv, mn_prev_bus;

static void mn_write(const char *s) {
	size_t n = strlen(s);
	while (n > 0) {
		ssize_t w = write(2, s, n);
		if (w <= 0)
			return;
		s += w;
		n -= (size_t)w;
	}
}

static void mn_chain(struct sigaction *prev, int sig, siginfo_t *info, void *uctx) {
	if (prev->sa_flags & SA_SIGINFO) {
		if (prev->sa_sigaction) {
			prev->sa_sigaction(sig, info, uctx);
			return;
		}
	} else if (prev->sa_handler != SIG_DFL && prev->sa_handler != SIG_IGN) {
		prev->sa_handler(sig);
		return;
	}
	// Типова дія: збій повториться після повернення і завершить процес.
	signal(sig, SIG_DFL);
}

static void mn_fault(int sig, siginfo_t *info, void *uctx) {
	owngo_mn_ctx *c = mn_cur;
	char *addr = (char *)info->si_addr;
	if (c && c->map && addr >= c->map && addr < c->lo) {
		char buf[160];
		snprintf(buf, sizeof buf,
			"runtime: goroutine stack exceeds %zu-byte limit\n"
			"fatal error: stack overflow\n\ngoroutine %llu [running]\n",
			c->size, (unsigned long long)c->goid);
		mn_write(buf);
		_exit(2);
	}
	mn_chain(sig == SIGBUS ? &mn_prev_bus : &mn_prev_segv, sig, info, uctx);
}

static void mn_install(void) {
	mn_page = sysconf(_SC_PAGESIZE);
	struct sigaction sa;
	memset(&sa, 0, sizeof sa);
	sa.sa_sigaction = mn_fault;
	sigemptyset(&sa.sa_mask);
	sa.sa_flags = SA_SIGINFO | SA_ONSTACK | SA_NODEFER;
	sigaction(SIGSEGV, &sa, &mn_prev_segv);
	sigaction(SIGBUS, &sa, &mn_prev_bus);
}

// mn_thread_init дає потоку альтернативний стек сигналів: обробник збою на
// вичерпаному стеку G інакше не має де виконатися.
static void mn_thread_init(void) {
	if (mn_altstack_set)
		return;
	mn_altstack_set = 1;
	void *p = mmap(NULL, MN_ALTSTACK, PROT_READ | PROT_WRITE, MAP_PRIVATE | MAP_ANON, -1, 0);
	if (p == MAP_FAILED)
		return;
	stack_t ss;
	memset(&ss, 0, sizeof ss);
	ss.ss_sp = p;
	ss.ss_size = MN_ALTSTACK;
	sigaltstack(&ss, NULL);
}

// ---- пул стеків ------------------------------------------------------------

static pthread_mutex_t mn_pool_lock = PTHREAD_MUTEX_INITIALIZER;
static owngo_mn_ctx *mn_hot, *mn_cold; // вільні стеки: сторінки при собі і віддані ОС
static size_t mn_hot_n;
static size_t mn_pool_size; // розмір стека в пулі: пул тримає один розмір

size_t owngo_mn_default_stack(void) {
	pthread_attr_t a;
	size_t n = 0;
	if (pthread_attr_init(&a) == 0) {
		pthread_attr_getstacksize(&a, &n);
		pthread_attr_destroy(&a);
	}
	return n ? n : (size_t)8 << 20;
}

static owngo_mn_ctx *mn_pool_take(size_t size) {
	owngo_mn_ctx *c = NULL;
	pthread_mutex_lock(&mn_pool_lock);
	if (mn_pool_size == size) {
		if (mn_hot) {
			c = mn_hot;
			mn_hot = c->next;
			mn_hot_n--;
		} else if (mn_cold) {
			c = mn_cold;
			mn_cold = c->next;
		}
	}
	pthread_mutex_unlock(&mn_pool_lock);
	return c;
}

// owngo_mn_ctx_new дає контекст, який першим перемиканням на нього викличе
// fn(arg) на власному стеку розміру size (0 — як типовий стек потоку).
// fn не повертається: G завершується owngo_mn_exit. NULL — резерву немає.
owngo_mn_ctx *owngo_mn_ctx_new(size_t size, void *(*fn)(void *), void *arg, uint64_t goid) {
	pthread_once(&mn_once, mn_install);
	if (size == 0)
		size = owngo_mn_default_stack();
	size = (size + (size_t)mn_page - 1) & ~((size_t)mn_page - 1);
	owngo_mn_ctx *c = mn_pool_take(size);
	if (!c) {
		size_t mapsize = OWNGO_MN_GUARD + size;
		int flags = MAP_PRIVATE | MAP_ANON;
#ifdef MAP_NORESERVE
		flags |= MAP_NORESERVE;
#endif
#ifdef MAP_STACK
		flags |= MAP_STACK;
#endif
		char *map = mmap(NULL, mapsize, PROT_READ | PROT_WRITE, flags, -1, 0);
		if (map == MAP_FAILED)
			return NULL;
		if (mprotect(map, OWNGO_MN_GUARD, PROT_NONE) != 0) {
			munmap(map, mapsize);
			return NULL;
		}
		// Заголовок — на вершині резерву, стек G — під ним.
		c = (owngo_mn_ctx *)(map + mapsize - ((sizeof(owngo_mn_ctx) + 15) & ~(size_t)15));
		c->map = map;
		c->mapsize = mapsize;
		c->lo = map + OWNGO_MN_GUARD;
	}
	char *top = (char *)c;
	c->size = (size_t)(top - c->lo);
	c->sp = mn_init_frame(top);
	c->fake = NULL;
	c->goid = goid;
	c->fn = fn;
	c->arg = arg;
	c->next = NULL;
	return c;
}

// owngo_mn_ctx_free повертає стек у пул. Контекст не має виконуватися.
void owngo_mn_ctx_free(owngo_mn_ctx *c) {
	if (!c || !c->map)
		return;
	size_t size = c->mapsize - OWNGO_MN_GUARD;
	pthread_mutex_lock(&mn_pool_lock);
	if (mn_pool_size != size) {
		if (mn_hot || mn_cold) {
			// Стек іншого розміру, ніж у пулі, віддається ОС цілим.
			pthread_mutex_unlock(&mn_pool_lock);
			munmap(c->map, c->mapsize);
			return;
		}
		mn_pool_size = size;
	}
	if (mn_hot_n < MN_POOL_HOT) {
		c->next = mn_hot;
		mn_hot = c;
		mn_hot_n++;
		pthread_mutex_unlock(&mn_pool_lock);
		return;
	}
	pthread_mutex_unlock(&mn_pool_lock);
	// Холодний стек: сторінки — ОС, резерв лишається. Заголовок лежить на
	// останній сторінці, її не чіпаємо.
	char *lo = c->map + OWNGO_MN_GUARD;
	size_t n = (size_t)((char *)c - lo) & ~((size_t)mn_page - 1);
	madvise(lo, n, MADV_DONTNEED);
	pthread_mutex_lock(&mn_pool_lock);
	c->next = mn_cold;
	mn_cold = c;
	pthread_mutex_unlock(&mn_pool_lock);
}

// owngo_mn_thread_ctx — контекст власного стека поточного потоку.
owngo_mn_ctx *owngo_mn_thread_ctx(void) {
	return &mn_thread;
}

// owngo_mn_current — контекст, що виконується; NULL — стек потоку.
owngo_mn_ctx *owngo_mn_current(void) {
	return mn_cur;
}

// mn_arrived — перші дії в контексті після перемикання на нього: ASan
// дізнається, що старий стек покинуто, і повідомляє його межі; межі стека
// потоку відомі лише так.
static void mn_arrived(void *fake) {
	if (!__sanitizer_finish_switch_fiber)
		return;
	const void *lo = NULL;
	size_t size = 0;
	__sanitizer_finish_switch_fiber(fake, &lo, &size);
	owngo_mn_ctx *from = mn_from;
	if (from && !from->map && from->size == 0) {
		from->lo = (char *)lo;
		from->size = size;
	}
}

// Межі стека для обходу кадрів (runtime.Caller, debug.Stack, звіт паніки):
// runtime/internal/stacktrace/_wrap/traceback_unix.c.
void llgo_traceback_set_stack(uintptr_t low, uintptr_t high);

static void mn_switch(owngo_mn_ctx *from, owngo_mn_ctx *to, int exiting) {
	mn_thread_init();
	if (to->map)
		llgo_traceback_set_stack((uintptr_t)to->lo, (uintptr_t)to->lo + to->size);
	else
		llgo_traceback_set_stack(0, 0);
	if (__sanitizer_start_switch_fiber)
		__sanitizer_start_switch_fiber(exiting ? NULL : &from->fake, to->lo, to->size);
	mn_from = from;
	mn_cur = to == &mn_thread ? NULL : to;
	owngo_mn_swap(&from->sp, to->sp);
	mn_arrived(from->fake);
}

// owngo_mn_switch зберігає from і продовжує to; повертається, коли хтось
// перемкне назад на from.
void owngo_mn_switch(owngo_mn_ctx *from, owngo_mn_ctx *to) {
	mn_switch(from, to, 0);
}

// owngo_mn_exit — останнє перемикання з from: from більше не виконується,
// і його стек можна звільнити з to.
void owngo_mn_exit(owngo_mn_ctx *from, owngo_mn_ctx *to) {
	mn_switch(from, to, 1);
	static const char msg[] = "fatal error: owngo_mn_exit returned\n";
	mn_write(msg);
	_exit(2);
}

// owngo_mn_begin — перші дії нового контексту на його стеку (з
// owngo_mn_start); повертає сам контекст.
owngo_mn_ctx *owngo_mn_begin(void) {
	mn_arrived(NULL);
	return mn_cur;
}

// ---- очікування й процесори для планувальника (P-NEW-09.6) -----------------

#include <stdlib.h>
#include <time.h>
#if defined(__linux__)
#include <linux/futex.h>
#include <sched.h>
#include <sys/syscall.h>
#endif

// owngo_mn_wait спить, доки *addr == val і ніхто не розбудив; ns < 0 — без
// межі. 0 — розбуджено (або значення вже інше), 1 — минув час.
int owngo_mn_wait(uint32_t *addr, uint32_t val, int64_t ns) {
#if defined(__linux__)
	struct timespec ts, *tp = NULL;
	if (ns >= 0) {
		ts.tv_sec = (time_t)(ns / 1000000000);
		ts.tv_nsec = (long)(ns % 1000000000);
		tp = &ts;
	}
	long r = syscall(SYS_futex, addr, FUTEX_WAIT_PRIVATE, val, tp, NULL, 0);
	return r != 0 && errno == ETIMEDOUT;
#elif defined(__APPLE__)
	extern int __ulock_wait(uint32_t op, void *addr, uint64_t value, uint32_t timeout_us);
	uint32_t us = 0;
	if (ns >= 0)
		us = ns / 1000 > 0xffffffffu ? 0xffffffffu : (ns / 1000 == 0 ? 1 : (uint32_t)(ns / 1000));
	int r = __ulock_wait(1 /* UL_COMPARE_AND_WAIT */, addr, val, us);
	return r < 0 && errno == ETIMEDOUT;
#endif
}

void owngo_mn_wake(uint32_t *addr, int all) {
#if defined(__linux__)
	syscall(SYS_futex, addr, FUTEX_WAKE_PRIVATE, all ? 0x7fffffff : 1, NULL, NULL, 0);
#elif defined(__APPLE__)
	extern int __ulock_wake(uint32_t op, void *addr, uint64_t wake_value);
	__ulock_wake(1 | (all ? 0x100 /* ULF_WAKE_ALL */ : 0), addr, 0);
#endif
}

// owngo_mn_procs — типове GOMAXPROCS, як у Go 1.25+: змінна середовища
// GOMAXPROCS, інакше процесори, доступні потоку (sched_getaffinity), обмежені
// квотою cgroup v2 (cpu.max, округлення вгору).
int owngo_mn_procs(void) {
	const char *env = getenv("GOMAXPROCS");
	if (env && *env) {
		int n = atoi(env);
		if (n > 0)
			return n;
	}
	long n = sysconf(_SC_NPROCESSORS_ONLN);
#if defined(__linux__)
	cpu_set_t set;
	if (sched_getaffinity(0, sizeof set, &set) == 0 && CPU_COUNT(&set) > 0)
		n = CPU_COUNT(&set);
	FILE *f = fopen("/sys/fs/cgroup/cpu.max", "r");
	if (f) {
		char quota[32];
		long period = 0;
		if (fscanf(f, "%31s %ld", quota, &period) == 2 && strcmp(quota, "max") != 0 && period > 0) {
			long q = atol(quota);
			long limit = (q + period - 1) / period;
			if (limit > 0 && limit < n)
				n = limit;
		}
		fclose(f);
	}
#endif
	return n > 0 ? (int)n : 1;
}
