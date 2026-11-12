// Алокатор owngo (P8.3): size-class з пошуком бази внутрішнього вказівника.
//
// Регіон адрес резервується одним mmap і ріжеться на сторінки по 64 КіБ.
// Мала сторінка належить одному класу (крок 16 байтів, до 8 КіБ), тож
// база = початок сторінки + номер блока × розмір класу. Великий блок займає
// кілька цілих сторінок, і кожна з них у таблиці вказує на першу.
//
// Мала сторінка належить купі (heap). Купа — набір сторінок одного потоку;
// коли потік завершується, купа стає покинутою, і її бере наступний новий
// потік разом з усіма сторінками. Купа ніколи не звільняється, тож власник
// сторінки завжди дійсний.
//
// Блок, звільнений власником купи, йде в локальний список сторінки. Блок
// із чужого потоку йде в атомарний список remote сторінки; перший такий
// блок ще й кладе сторінку в атомарний стек pending купи. Купа забирає
// pending, коли поточна сторінка класу вичерпана. Сторінка в pending
// лежить, доки її remote не порожній, а порожнім його робить лише власник,
// тож поле pending_next сторінки не перезаписується, поки власник його читає.
//
// Порожня мала сторінка, яка не є поточною для класу, і звільнений великий
// блок повертаються в спільний пул вільних діапазонів сторінок. Діапазон
// у пулі лишається зайнятим (P-NEW-08.2): наступне виділення бере його без
// сторінкових збоїв. Діапазон, що пролежав у пулі довше за
// SC_PURGE_DELAY_NS, віддається системі через madvise, доки зайнятих
// сторінок пулу більше за SC_POOL_KEEP. Перевірка віку йде під sc_lock на
// кожному взятті чи поверненні сторінок, не частіше ніж раз на половину
// затримки, тож програма, яка більше не звертається до алокатора, тримає
// пул до наступного звертання. Діапазон із пулу береться першим, що
// підходить, з поділом.
//
// З OWNGO_ALLOC_STAT (файл owngo_alloc_stat.c, тег owngostat) алокатор
// рахує свої події для рядків owngo alloc (P-NEW-08.1). Події власника купи
// лежать у самій купі без атомарних операцій; події під sc_lock — у
// sc_stat; remote-звільнення — в атомарному лічильнику. Без макроса
// лічильників немає.

#include <pthread.h>
#include <stdatomic.h>
#include <stddef.h>
#include <stdint.h>
#include <stdlib.h>
#include <sys/mman.h>
#include <sys/resource.h>
#include <time.h>

#define SC_PAGE_SHIFT 16
#define SC_PAGE ((size_t)1 << SC_PAGE_SHIFT)
#define SC_REGION_MAX ((size_t)1 << 36)
#define SC_REGION_MIN ((size_t)1 << 26)
#define SC_STEP 16
#define SC_SMALL_MAX 8192
#define SC_CLASSES (SC_SMALL_MAX / SC_STEP + 1)
#define SC_POOL_KEEP 64
// Затримка повернення сторінок — 10 мс, як типова purge_delay у mimalloc.
// На binary-trees 21 затримки 10 мс, 100 мс і 1 с дали той самий час і
// sys у межах розкиду (owngo/REPORT-STAGEF.md), тож обрано найкоротшу.
#define SC_PURGE_DELAY_NS ((uint64_t)10 * 1000 * 1000)
#define SC_NONE SIZE_MAX

enum { SC_FREE, SC_SMALL, SC_LARGE };

typedef struct sc_heap sc_heap;
typedef struct sc_page sc_page;

// Метадані сторінок різних потоків лежать у таблиці поруч і змінюються
// на кожному виділенні, тож кожна займає свої дві кеш-лінії (128 байтів),
// без false sharing.
struct sc_page {
	_Alignas(128) uint32_t kind;
	uint32_t size;   // розмір класу малої сторінки
	uint32_t start;  // перша сторінка великого блока чи діапазону пулу
	uint32_t npages; // сторінок у великому блоці чи діапазоні (на першій)

	// Мала сторінка; поля нижче, крім remote і pending_next, змінює лише власник.
	sc_heap *owner;
	void *free;
	char *bump;
	char *end;
	uint32_t used; // виділені блоки, включно з тими, що лежать у remote
	uint32_t in_partial;
	sc_page *prev, *next; // список partial класу
	_Atomic(void *) remote;
	sc_page *pending_next;

	// Діапазон пулу.
	sc_page *next_run;
	uint32_t committed;
	uint64_t freed_at; // мітка sc_now, коли діапазон потрапив у пул
};

// Номери лічильників; порядок — порядок рядків owngo alloc.
enum {
	SC_STAT_SMALL,         // owngo_malloc малого блока
	SC_STAT_LARGE,         // owngo_malloc великого блока
	SC_STAT_FREE_LOCAL,    // owngo_free малого блока власником купи
	SC_STAT_FREE_REMOTE,   // owngo_free малого блока іншим потоком
	SC_STAT_FREE_LARGE,    // owngo_free великого блока
	SC_STAT_PAGE_NEW,      // сторінки за межею використаних
	SC_STAT_PAGE_REUSE,    // сторінки з пулу
	SC_STAT_PAGE_GIVE,     // сторінки, повернуті в пул
	SC_STAT_MADVISE,       // виклики madvise
	SC_STAT_MADVISE_BYTES, // байти madvise
	SC_STAT_LOCK,          // захоплення sc_lock
	SC_STAT_COUNT
};

struct sc_heap {
	sc_page *cur[SC_CLASSES];
	sc_page *partial[SC_CLASSES];
	_Atomic(sc_page *) pending;
	sc_heap *next_abandoned;
#ifdef OWNGO_ALLOC_STAT
	sc_heap *next_all; // усі купи, під sc_lock
	// Пише лише власник; atomic лише щоб читання на виході не було гонкою.
	_Atomic uint64_t stat[SC_STAT_COUNT];
#endif
};

static char *sc_region;
static size_t sc_pages; // сторінок у регіоні
static sc_page *sc_table;
static atomic_size_t sc_used_pages; // межа використаних сторінок регіону

static pthread_once_t sc_once = PTHREAD_ONCE_INIT;
static pthread_key_t sc_key;
static pthread_mutex_t sc_lock = PTHREAD_MUTEX_INITIALIZER;
static sc_page *sc_runs;         // вільні діапазони пулу, під sc_lock
static size_t sc_committed;      // зайняті сторінки пулу, під sc_lock
static uint64_t sc_next_purge;   // sc_now наступної перевірки віку, під sc_lock
static sc_heap *sc_abandoned;    // покинуті купи, під sc_lock

static __thread sc_heap *sc_tls;

#ifdef OWNGO_ALLOC_STAT
static sc_heap *sc_all;                 // під sc_lock
static uint64_t sc_stat[SC_STAT_COUNT]; // під sc_lock
static _Atomic uint64_t sc_stat_remote;

static inline void sc_stat_heap(sc_heap *h, int i) {
	uint64_t v = atomic_load_explicit(&h->stat[i], memory_order_relaxed);
	atomic_store_explicit(&h->stat[i], v + 1, memory_order_relaxed);
}
#define SC_STAT_HEAP(h, i) sc_stat_heap(h, i)
#define SC_STAT_LOCKED(i, n) (sc_stat[i] += (n))
#define SC_STAT_REMOTE() atomic_fetch_add_explicit(&sc_stat_remote, 1, memory_order_relaxed)
#else
#define SC_STAT_HEAP(h, i) ((void)0)
#define SC_STAT_LOCKED(i, n) ((void)0)
#define SC_STAT_REMOTE() ((void)0)
#endif

static inline void sc_lock_acquire(void) {
	pthread_mutex_lock(&sc_lock);
	SC_STAT_LOCKED(SC_STAT_LOCK, 1);
}

static void sc_abandon(void *arg) {
	sc_heap *h = arg;
	sc_tls = NULL;
	sc_lock_acquire();
	h->next_abandoned = sc_abandoned;
	sc_abandoned = h;
	pthread_mutex_unlock(&sc_lock);
}

// sc_init резервує регіон до SC_REGION_MAX, але не більше половини
// RLIMIT_AS: резерв рахується в ліміт навіть з MAP_NORESERVE. Якщо mmap
// відмовляє, регіон удвічі менший, до SC_REGION_MIN.
static void sc_init(void) {
	size_t size = SC_REGION_MAX;
	struct rlimit lim;
	if (getrlimit(RLIMIT_AS, &lim) == 0 && lim.rlim_cur != RLIM_INFINITY &&
	    lim.rlim_cur / 2 < size) {
		size = (size_t)lim.rlim_cur / 2 & ~(SC_PAGE - 1);
	}
	for (; size >= SC_REGION_MIN; size /= 2) {
		sc_pages = size >> SC_PAGE_SHIFT;
		sc_region = mmap(NULL, size, PROT_READ | PROT_WRITE,
		                 MAP_PRIVATE | MAP_ANONYMOUS | MAP_NORESERVE, -1, 0);
		if (sc_region == MAP_FAILED) {
			continue;
		}
		sc_table = mmap(NULL, sc_pages * sizeof(sc_page), PROT_READ | PROT_WRITE,
		                MAP_PRIVATE | MAP_ANONYMOUS | MAP_NORESERVE, -1, 0);
		if (sc_table != MAP_FAILED) {
			break;
		}
		munmap(sc_region, size);
	}
	if (size < SC_REGION_MIN || pthread_key_create(&sc_key, sc_abandon) != 0) {
		abort();
	}
}

static inline size_t sc_index(const void *p) {
	return (size_t)((const char *)p - sc_region) >> SC_PAGE_SHIFT;
}

static inline char *sc_addr(size_t idx) {
	return sc_region + (idx << SC_PAGE_SHIFT);
}

static uint64_t sc_now(void) {
	struct timespec ts;
	clock_gettime(CLOCK_MONOTONIC, &ts);
	return (uint64_t)ts.tv_sec * 1000000000u + (uint64_t)ts.tv_nsec;
}

// sc_purge віддає системі зайняті діапазони пулу, старші за
// SC_PURGE_DELAY_NS, доки зайнятих сторінок пулу більше за SC_POOL_KEEP.
// Викликається під sc_lock.
static void sc_purge(void) {
	uint64_t now = sc_now();
	if (now < sc_next_purge) {
		return;
	}
	sc_next_purge = now + SC_PURGE_DELAY_NS / 2;
	for (sc_page *run = sc_runs; run != NULL && sc_committed > SC_POOL_KEEP; run = run->next_run) {
		if (!run->committed || now - run->freed_at < SC_PURGE_DELAY_NS) {
			continue;
		}
		madvise(sc_addr(run->start), (size_t)run->npages << SC_PAGE_SHIFT, MADV_DONTNEED);
		SC_STAT_LOCKED(SC_STAT_MADVISE, 1);
		SC_STAT_LOCKED(SC_STAT_MADVISE_BYTES, (size_t)run->npages << SC_PAGE_SHIFT);
		run->committed = 0;
		sc_committed -= run->npages;
	}
}

// sc_take_pages повертає першу сторінку n вільних поспіль: з пулу чи
// за межею використаних; SC_NONE — регіон вичерпано. Викликається під sc_lock.
static size_t sc_take_pages(size_t n) {
	sc_purge();
	for (sc_page **link = &sc_runs; *link != NULL; link = &(*link)->next_run) {
		sc_page *run = *link;
		if (run->npages < n) {
			continue;
		}
		size_t idx = run->start;
		if (run->committed) {
			sc_committed -= n;
		}
		SC_STAT_LOCKED(SC_STAT_PAGE_REUSE, n);
		if (run->npages == n) {
			*link = run->next_run;
		} else {
			sc_page *rest = &sc_table[idx + n];
			rest->kind = SC_FREE;
			rest->start = (uint32_t)(idx + n);
			rest->npages = run->npages - (uint32_t)n;
			rest->committed = run->committed;
			rest->freed_at = run->freed_at;
			rest->next_run = run->next_run;
			*link = rest;
		}
		return idx;
	}
	size_t idx = atomic_load_explicit(&sc_used_pages, memory_order_relaxed);
	if (idx + n > sc_pages) {
		return SC_NONE;
	}
	atomic_store_explicit(&sc_used_pages, idx + n, memory_order_release);
	SC_STAT_LOCKED(SC_STAT_PAGE_NEW, n);
	return idx;
}

// sc_give_pages повертає n сторінок з idx у пул. Викликається під sc_lock.
static void sc_give_pages(size_t idx, size_t n) {
	sc_page *run = &sc_table[idx];
	run->kind = SC_FREE;
	run->start = (uint32_t)idx;
	run->npages = (uint32_t)n;
	run->committed = 1;
	run->freed_at = sc_now();
	sc_committed += n;
	SC_STAT_LOCKED(SC_STAT_PAGE_GIVE, n);
	run->next_run = sc_runs;
	sc_runs = run;
	sc_purge();
}


static sc_heap *sc_heap_get(void);

// owngo_sc_attach робить купу *held кешем цього потоку. Нульова *held —
// поточна купа потоку або нова; вона записується в *held і лишається за
// викликачем (P). Вихід потоку її не кидає.
void owngo_sc_attach(void **held) {
	pthread_once(&sc_once, sc_init);
	sc_heap *h = held != NULL ? *held : NULL;
	if (h == NULL) {
		h = sc_tls;
		if (h == NULL)
			h = sc_heap_get();
		if (held != NULL)
			*held = h;
	}
	sc_tls = h;
	pthread_setspecific(sc_key, NULL);
}

// owngo_sc_detach знімає кеш з потоку, не віддаючи купу. Наступний потік,
// що візьме той самий P, чіпляє її знову.
void owngo_sc_detach(void) {
	if (sc_tls == NULL)
		return;
	sc_tls = NULL;
	pthread_setspecific(sc_key, NULL);
}

static sc_heap *sc_heap_get(void) {
	pthread_once(&sc_once, sc_init);
	sc_lock_acquire();
	sc_heap *h = sc_abandoned;
	if (h != NULL) {
		sc_abandoned = h->next_abandoned;
	}
	pthread_mutex_unlock(&sc_lock);
	if (h == NULL) {
		h = mmap(NULL, sizeof(sc_heap), PROT_READ | PROT_WRITE,
		         MAP_PRIVATE | MAP_ANONYMOUS, -1, 0);
		if (h == MAP_FAILED) {
			abort();
		}
#ifdef OWNGO_ALLOC_STAT
		pthread_mutex_lock(&sc_lock);
		h->next_all = sc_all;
		sc_all = h;
		pthread_mutex_unlock(&sc_lock);
#endif
	}
	sc_tls = h;
	pthread_setspecific(sc_key, h);
	return h;
}

static void sc_partial_add(sc_heap *h, sc_page *pg) {
	size_t c = pg->size / SC_STEP;
	pg->in_partial = 1;
	pg->prev = NULL;
	pg->next = h->partial[c];
	if (pg->next != NULL) {
		pg->next->prev = pg;
	}
	h->partial[c] = pg;
}

static void sc_partial_remove(sc_heap *h, sc_page *pg) {
	if (pg->prev != NULL) {
		pg->prev->next = pg->next;
	} else {
		h->partial[pg->size / SC_STEP] = pg->next;
	}
	if (pg->next != NULL) {
		pg->next->prev = pg->prev;
	}
	pg->in_partial = 0;
}

// sc_page_changed ставить сторінку, якій власник щойно повернув блоки, на
// місце: порожня йде в пул, інша — у partial. Поточна сторінка класу
// лишається поточною.
static void sc_page_changed(sc_heap *h, sc_page *pg) {
	if (pg == h->cur[pg->size / SC_STEP]) {
		return;
	}
	if (pg->used != 0) {
		if (!pg->in_partial) {
			sc_partial_add(h, pg);
		}
		return;
	}
	if (pg->in_partial) {
		sc_partial_remove(h, pg);
	}
	pg->kind = SC_FREE;
	pg->owner = NULL;
	sc_lock_acquire();
	sc_give_pages((size_t)(pg - sc_table), 1);
	pthread_mutex_unlock(&sc_lock);
}

// sc_collect забирає блоки, звільнені чужими потоками.
static void sc_collect(sc_heap *h) {
	sc_page *pg = atomic_exchange_explicit(&h->pending, NULL, memory_order_acquire);
	while (pg != NULL) {
		sc_page *next = pg->pending_next;
		void *list = atomic_exchange_explicit(&pg->remote, NULL, memory_order_acquire);
		while (list != NULL) {
			void *rest = *(void **)list;
			*(void **)list = pg->free;
			pg->free = list;
			pg->used--;
			list = rest;
		}
		sc_page_changed(h, pg);
		pg = next;
	}
}

static sc_page *sc_new_page(sc_heap *h, size_t c) {
	sc_lock_acquire();
	size_t idx = sc_take_pages(1);
	pthread_mutex_unlock(&sc_lock);
	if (idx == SC_NONE) {
		return NULL;
	}
	sc_page *pg = &sc_table[idx];
	size_t sz = c * SC_STEP;
	pg->kind = SC_SMALL;
	pg->size = (uint32_t)sz;
	pg->start = (uint32_t)idx;
	pg->npages = 1;
	pg->owner = h;
	pg->free = NULL;
	pg->bump = sc_addr(idx);
	pg->end = pg->bump + (SC_PAGE / sz) * sz;
	pg->used = 0;
	pg->in_partial = 0;
	atomic_store_explicit(&pg->remote, NULL, memory_order_relaxed);
	return pg;
}

static void *sc_alloc_slow(sc_heap *h, size_t c) {
	sc_collect(h);
	sc_page *pg = h->cur[c];
	if (pg == NULL || (pg->free == NULL && pg->bump == pg->end)) {
		pg = h->partial[c];
		if (pg != NULL) {
			sc_partial_remove(h, pg);
		} else {
			pg = sc_new_page(h, c);
			if (pg == NULL) {
				return NULL;
			}
		}
		h->cur[c] = pg;
	}
	void *p = pg->free;
	if (p != NULL) {
		pg->free = *(void **)p;
	} else {
		p = pg->bump;
		pg->bump += pg->size;
	}
	pg->used++;
	SC_STAT_HEAP(h, SC_STAT_SMALL);
	return p;
}

static void *sc_alloc_large(size_t size) {
	pthread_once(&sc_once, sc_init);
	size_t n = (size + SC_PAGE - 1) >> SC_PAGE_SHIFT;
	sc_lock_acquire();
	size_t idx = sc_take_pages(n);
	if (idx != SC_NONE) {
		SC_STAT_LOCKED(SC_STAT_LARGE, 1);
	}
	pthread_mutex_unlock(&sc_lock);
	if (idx == SC_NONE) {
		return NULL;
	}
	for (size_t i = 0; i < n; i++) {
		sc_page *pg = &sc_table[idx + i];
		pg->kind = SC_LARGE;
		pg->size = 0;
		pg->start = (uint32_t)idx;
		pg->npages = (uint32_t)n;
	}
	return sc_addr(idx);
}

// owngo_malloc повертає блок не менше size байтів, вирівняний на 16.
// Нульовий size рахується як 1. Розмір понад SC_REGION_MAX дає NULL.
void *owngo_malloc(size_t size) {
	if (size > SC_SMALL_MAX) {
		if (size > SC_REGION_MAX) {
			return NULL;
		}
		return sc_alloc_large(size);
	}
	size_t c = size == 0 ? 1 : (size + SC_STEP - 1) / SC_STEP;
	sc_heap *h = sc_tls;
	if (h == NULL) {
		h = sc_heap_get();
	}
	sc_page *pg = h->cur[c];
	if (pg != NULL) {
		void *p = pg->free;
		if (p != NULL) {
			pg->free = *(void **)p;
			pg->used++;
			SC_STAT_HEAP(h, SC_STAT_SMALL);
			return p;
		}
		if (pg->bump != pg->end) {
			p = pg->bump;
			pg->bump += pg->size;
			pg->used++;
			SC_STAT_HEAP(h, SC_STAT_SMALL);
			return p;
		}
	}
	return sc_alloc_slow(h, c);
}

// owngo_free звільняє блок owngo_malloc з будь-якого потоку.
void owngo_free(void *p) {
	sc_page *pg = &sc_table[sc_index(p)];
	if (pg->kind == SC_LARGE) {
		size_t idx = pg->start;
		size_t n = pg->npages;
		sc_lock_acquire();
		SC_STAT_LOCKED(SC_STAT_FREE_LARGE, 1);
		sc_give_pages(idx, n);
		pthread_mutex_unlock(&sc_lock);
		return;
	}
	sc_heap *h = pg->owner;
	if (h == sc_tls) {
		SC_STAT_HEAP(h, SC_STAT_FREE_LOCAL);
		*(void **)p = pg->free;
		pg->free = p;
		pg->used--;
		if (pg->used == 0 || !pg->in_partial) {
			sc_page_changed(h, pg);
		}
		return;
	}
	SC_STAT_REMOTE();
	void *old = atomic_load_explicit(&pg->remote, memory_order_relaxed);
	do {
		*(void **)p = old;
	} while (!atomic_compare_exchange_weak_explicit(&pg->remote, &old, p,
	                                                memory_order_release, memory_order_relaxed));
	if (old != NULL) {
		return;
	}
	sc_page *head = atomic_load_explicit(&h->pending, memory_order_relaxed);
	do {
		pg->pending_next = head;
	} while (!atomic_compare_exchange_weak_explicit(&h->pending, &head, pg,
	                                                memory_order_release, memory_order_relaxed));
}

// owngo_base повертає початок блока owngo_malloc, всередині якого лежить p,
// і NULL для адреси поза регіоном.
void *owngo_base(const void *p) {
	const char *q = p;
	size_t used = atomic_load_explicit(&sc_used_pages, memory_order_acquire);
	if (sc_region == NULL || q < sc_region || q >= sc_addr(used)) {
		return NULL;
	}
	sc_page *pg = &sc_table[sc_index(q)];
	char *page = sc_addr(pg->start);
	switch (pg->kind) {
	case SC_LARGE:
		return page;
	case SC_SMALL:
		return page + (size_t)(q - page) / pg->size * pg->size;
	default:
		return NULL;
	}
}

#ifdef OWNGO_ALLOC_STAT
// owngo_alloc_stat пише в out[SC_STAT_COUNT] лічильники за весь процес:
// суму куп усіх потоків, лічильники під sc_lock і remote-звільнення.
// Читання не захоплює sc_lock через sc_lock_acquire, тож себе не рахує.
void owngo_alloc_stat(uint64_t *out) {
	pthread_once(&sc_once, sc_init);
	pthread_mutex_lock(&sc_lock);
	for (int i = 0; i < SC_STAT_COUNT; i++) {
		out[i] = sc_stat[i];
	}
	for (sc_heap *h = sc_all; h != NULL; h = h->next_all) {
		for (int i = 0; i < SC_STAT_COUNT; i++) {
			out[i] += atomic_load_explicit(&h->stat[i], memory_order_relaxed);
		}
	}
	pthread_mutex_unlock(&sc_lock);
	out[SC_STAT_FREE_REMOTE] += atomic_load_explicit(&sc_stat_remote, memory_order_relaxed);
}
#endif
