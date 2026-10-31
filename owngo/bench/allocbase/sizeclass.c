// Мінімальний прототип алокатора size-class з пошуком бази (P8.2).
//
// Регіон адрес резервується одним mmap і ріжеться на сторінки по 64 КіБ.
// Сторінка малого блока належить одному класу (крок 16 байтів, до 8 КіБ):
// база = початок сторінки + номер блока × розмір класу. Великий блок займає
// кілька цілих сторінок, і кожна з них у таблиці вказує на першу.
// Кожен потік має свої вільні списки й поточну сторінку кожного класу.
// Звільнений великий блок віддає пам'ять через madvise, адреси не повторює.

#include <stdatomic.h>
#include <stddef.h>
#include <stdint.h>
#include <stdlib.h>
#include <sys/mman.h>

#include "sizeclass.h"

#define SC_PAGE_SHIFT 16
#define SC_PAGE ((size_t)1 << SC_PAGE_SHIFT)
#define SC_REGION ((size_t)1 << 36)
#define SC_PAGES (SC_REGION >> SC_PAGE_SHIFT)
#define SC_STEP 16
#define SC_SMALL_MAX 8192
#define SC_CLASSES (SC_SMALL_MAX / SC_STEP + 1)

typedef struct {
	uint32_t size;   // розмір класу малого блока; 0 — сторінка великого
	uint32_t start;  // перша сторінка великого блока
	uint32_t npages; // кількість сторінок великого блока (на першій)
} sc_page;

static char *sc_region;
static sc_page *sc_table;
static atomic_size_t sc_next;

static __thread void *sc_free[SC_CLASSES];
static __thread char *sc_bump[SC_CLASSES];
static __thread char *sc_end[SC_CLASSES];

void sc_init(void) {
	sc_region = mmap(NULL, SC_REGION, PROT_READ | PROT_WRITE,
	                 MAP_PRIVATE | MAP_ANONYMOUS | MAP_NORESERVE, -1, 0);
	sc_table = mmap(NULL, SC_PAGES * sizeof(sc_page), PROT_READ | PROT_WRITE,
	                MAP_PRIVATE | MAP_ANONYMOUS | MAP_NORESERVE, -1, 0);
	if (sc_region == MAP_FAILED || sc_table == MAP_FAILED) {
		abort();
	}
}

static size_t sc_take(size_t n) {
	size_t idx = atomic_fetch_add(&sc_next, n);
	if (idx + n > SC_PAGES) {
		abort();
	}
	return idx;
}

void *sc_malloc(size_t size) {
	if (size == 0) {
		size = 1;
	}
	if (size > SC_SMALL_MAX) {
		size_t n = (size + SC_PAGE - 1) >> SC_PAGE_SHIFT;
		size_t idx = sc_take(n);
		for (size_t i = 0; i < n; i++) {
			sc_table[idx + i] = (sc_page){0, (uint32_t)idx, (uint32_t)n};
		}
		return sc_region + (idx << SC_PAGE_SHIFT);
	}
	size_t c = (size + SC_STEP - 1) / SC_STEP;
	void *p = sc_free[c];
	if (p != NULL) {
		sc_free[c] = *(void **)p;
		return p;
	}
	size_t sz = c * SC_STEP;
	if (sc_bump[c] == sc_end[c]) {
		size_t idx = sc_take(1);
		sc_table[idx] = (sc_page){(uint32_t)sz, (uint32_t)idx, 1};
		sc_bump[c] = sc_region + (idx << SC_PAGE_SHIFT);
		sc_end[c] = sc_bump[c] + (SC_PAGE / sz) * sz;
	}
	p = sc_bump[c];
	sc_bump[c] += sz;
	return p;
}

void sc_free_block(void *p) {
	size_t idx = (size_t)((char *)p - sc_region) >> SC_PAGE_SHIFT;
	sc_page e = sc_table[idx];
	if (e.size != 0) {
		size_t c = e.size / SC_STEP;
		*(void **)p = sc_free[c];
		sc_free[c] = p;
		return;
	}
	madvise(sc_region + ((size_t)e.start << SC_PAGE_SHIFT),
	        (size_t)e.npages << SC_PAGE_SHIFT, MADV_DONTNEED);
}

void *sc_base(const void *p) {
	const char *q = p;
	size_t used = atomic_load(&sc_next);
	if (q < sc_region || q >= sc_region + (used << SC_PAGE_SHIFT)) {
		return NULL;
	}
	size_t idx = (size_t)(q - sc_region) >> SC_PAGE_SHIFT;
	sc_page e = sc_table[idx];
	char *page = sc_region + ((size_t)e.start << SC_PAGE_SHIFT);
	if (e.size == 0) {
		return page;
	}
	return page + (size_t)(q - page) / e.size * e.size;
}
