// Алокатор owngo з семплером купи: теги owngostat і owngoheap (P-NEW-09.3).
//
// Кожне HP_RATE-те виділення потоку записує стек викликів (backtrace) і
// розмір; звільнення такого блока віднімає його від свого стека. На виході
// файл $OWNGO_HEAP_OUT отримує /proc/self/maps і рядки
// «site виділень байтів звільнень звільнених_байтів pc…»;
// owngo/bench/heapsym.py зводить їх у живі байти на стек. Без
// OWNGO_HEAP_OUT семплер працює, але нічого не пише.
//
// Таблиці — під одним м'ютексом, і кожне звільнення його бере: семплер
// уповільнює програму в рази і годиться лише для розкладу пам'яті.
#define owngo_malloc sc_real_malloc
#define owngo_free sc_real_free
#include "owngo_alloc_stat.c"
#undef owngo_malloc
#undef owngo_free

#include <execinfo.h>
#include <stdio.h>
#include <string.h>

#define HP_RATE 64
#define HP_DEPTH 10
#define HP_PTRS ((uint32_t)1 << 23) // відкрита адресація; живих семплів має бути менше
#define HP_SITES ((uint32_t)1 << 17)
#define HP_GONE ((void *)1) // звільнений слот таблиці вказівників

typedef struct {
	void *pc[HP_DEPTH];
	int n;
	uint64_t allocs, bytes, frees, freed_bytes;
} hp_site;

typedef struct {
	void *p;
	uint32_t site;
	uint32_t size;
} hp_ptr;

static hp_site hp_sites[HP_SITES];
static hp_ptr hp_ptrs[HP_PTRS];
static pthread_mutex_t hp_lock = PTHREAD_MUTEX_INITIALIZER;
static _Atomic int hp_any; // чи є хоч один семпл
static __thread uint32_t hp_tick;
static __thread int hp_busy; // backtrace може виділяти через malloc

static uint32_t hp_hash(const void *p) {
	uint64_t x = (uintptr_t)p;
	x ^= x >> 33;
	x *= 0xff51afd7ed558ccdULL;
	x ^= x >> 33;
	return (uint32_t)x;
}

static uint32_t hp_site_of(void **pc, int n) {
	uint64_t h = 1469598103934665603ULL;
	for (int i = 0; i < n; i++) {
		h = (h ^ (uintptr_t)pc[i]) * 1099511628211ULL;
	}
	for (uint32_t s = (uint32_t)h & (HP_SITES - 1);; s = (s + 1) & (HP_SITES - 1)) {
		hp_site *st = &hp_sites[s];
		if (st->n == 0) {
			memcpy(st->pc, pc, n * sizeof(void *));
			st->n = n;
			return s;
		}
		if (st->n == n && memcmp(st->pc, pc, n * sizeof(void *)) == 0) {
			return s;
		}
	}
}

void *owngo_malloc(size_t size) {
	void *p = sc_real_malloc(size);
	if (p == NULL || hp_busy || ++hp_tick % HP_RATE != 0) {
		return p;
	}
	hp_busy = 1;
	void *pc[HP_DEPTH + 1];
	int n = backtrace(pc, HP_DEPTH + 1) - 1; // без кадру owngo_malloc
	pthread_mutex_lock(&hp_lock);
	uint32_t s = hp_site_of(pc + 1, n);
	hp_sites[s].allocs++;
	hp_sites[s].bytes += size;
	uint32_t k = hp_hash(p) & (HP_PTRS - 1);
	while (hp_ptrs[k].p != NULL && hp_ptrs[k].p != HP_GONE) {
		k = (k + 1) & (HP_PTRS - 1);
	}
	hp_ptrs[k] = (hp_ptr){p, s, (uint32_t)size};
	hp_any = 1;
	pthread_mutex_unlock(&hp_lock);
	hp_busy = 0;
	return p;
}

void owngo_free(void *p) {
	if (hp_any) {
		pthread_mutex_lock(&hp_lock);
		for (uint32_t k = hp_hash(p) & (HP_PTRS - 1); hp_ptrs[k].p != NULL; k = (k + 1) & (HP_PTRS - 1)) {
			if (hp_ptrs[k].p == p) {
				hp_site *st = &hp_sites[hp_ptrs[k].site];
				st->frees++;
				st->freed_bytes += hp_ptrs[k].size;
				hp_ptrs[k].p = HP_GONE;
				break;
			}
		}
		pthread_mutex_unlock(&hp_lock);
	}
	sc_real_free(p);
}

__attribute__((destructor)) static void hp_dump(void) {
	const char *path = getenv("OWNGO_HEAP_OUT");
	if (path == NULL) {
		return;
	}
	FILE *out = fopen(path, "w");
	if (out == NULL) {
		return;
	}
	FILE *maps = fopen("/proc/self/maps", "r");
	char line[4096];
	while (maps != NULL && fgets(line, sizeof line, maps) != NULL) {
		fprintf(out, "map %s", line);
	}
	if (maps != NULL) {
		fclose(maps);
	}
	fprintf(out, "rate %d\n", HP_RATE);
	pthread_mutex_lock(&hp_lock);
	for (uint32_t s = 0; s < HP_SITES; s++) {
		hp_site *st = &hp_sites[s];
		if (st->n == 0) {
			continue;
		}
		fprintf(out, "site %llu %llu %llu %llu", (unsigned long long)st->allocs, (unsigned long long)st->bytes,
		        (unsigned long long)st->frees, (unsigned long long)st->freed_bytes);
		for (int i = 0; i < st->n; i++) {
			fprintf(out, " %lx", (unsigned long)(uintptr_t)st->pc[i]);
		}
		fputc('\n', out);
	}
	pthread_mutex_unlock(&hp_lock);
	fclose(out);
}
