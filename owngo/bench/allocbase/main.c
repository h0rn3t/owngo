// allocbase міряє кандидатів алокатора фази 8 (P8.2) однаково:
// правильність пошуку бази для внутрішнього вказівника, час пар
// malloc/free на одному і на восьми потоках, пікову RSS дерева.
//
//   allocbase <кандидат> base|seq|par|tree
//
// Кандидати: libc, bdwgc, owngo (алокатор рантайму,
// runtime/internal/runtime/_wrap/owngo_alloc.c). Збірка з -DALLOCBASE_MIMALLOC — лише
// mimalloc: системний libmimalloc підміняє malloc, тож у бінарнику libc його
// бути не може. Збірка з -fsanitize=address (-DALLOCBASE_ASAN) — лише asan,
// режим base.
// Код виходу 1 — неправильна база або невідомий аргумент.

#define GC_THREADS
#include <gc.h>

#include <pthread.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/resource.h>
#include <time.h>
#include <unistd.h>

// Алокатор owngo (P8.3) — runtime/internal/runtime/_wrap/owngo_alloc.c.
void *owngo_malloc(size_t size);
void owngo_free(void *p);
void *owngo_base(const void *p);

#ifdef ALLOCBASE_ASAN
#include <sanitizer/allocator_interface.h>
#endif

typedef struct {
	const char *name;
	void (*init)(void);
	void *(*alloc)(size_t);
	void (*free)(void *);
	void *(*base)(const void *); // NULL — пошуку бази немає
} candidate;

static void no_init(void) {}

static void gc_init(void) {
	GC_INIT();
	GC_disable();
}

// Збирання вимкнене, тож блок не скануватиметься: атомарний варіант
// не очищує пам'ять, як і malloc.
static void *gc_alloc(size_t n) { return GC_MALLOC_ATOMIC(n); }
static void gc_free(void *p) { GC_FREE(p); }
static void *gc_base(const void *p) { return GC_base((void *)p); }

#ifdef ALLOCBASE_MIMALLOC
#include <mimalloc.h>

// mi_block_base додає латка mimalloc-base.patch (-DALLOCBASE_MI_BASE).
// У системній бібліотеці пошуку бази немає.
#ifdef ALLOCBASE_MI_BASE
static void *mi_base(const void *p) { return mi_block_base(p); }
#else
#define mi_base NULL
#endif
#endif

#ifdef ALLOCBASE_ASAN
static void *asan_base(const void *p) {
	return (void *)__sanitizer_get_allocated_begin(p);
}
#endif

static const candidate candidates[] = {
#if defined(ALLOCBASE_ASAN)
	{"asan", no_init, malloc, free, asan_base},
#elif defined(ALLOCBASE_MIMALLOC)
	{"mimalloc", no_init, mi_malloc, mi_free, mi_base},
#else
	{"libc", no_init, malloc, free, NULL},
	{"bdwgc", gc_init, gc_alloc, gc_free, gc_base},
	{"owngo", no_init, owngo_malloc, owngo_free, owngo_base},
#endif
};

static const candidate *cand;

// zerobase стоїть замість однойменної змінної рантайму llgo.
static char zerobase;
static char global[64];

static int check(const char *what, const void *p, const void *want) {
	const void *got = cand->base(p);
	if (got == want) {
		printf("%s\tok\n", what);
		return 0;
	}
	printf("%s\tFAIL: base %p, want %p\n", what, got, want);
	return 1;
}

// Випадки бази з P8.2.
static int run_base(void) {
	if (cand->base == NULL) {
		printf("base\tnone\n");
		return 0;
	}
	int bad = 0;
	char stack[64];
	char *field = cand->alloc(24);
	char *array = cand->alloc(4096);
	char *last = cand->alloc(100);
	char *large = cand->alloc(2 << 20);
	memset(large, 1, 2 << 20);

	bad |= check("field+8", field + 8, field);
	bad |= check("array[300]", array + 300 * 8, array);
	bad |= check("last-byte", last + 99, last);
	bad |= check("large+1.5MiB", large + (3 << 19), large);
	bad |= check("global", global + 8, NULL);
	bad |= check("stack", stack + 8, NULL);
	bad |= check("zerobase", &zerobase, NULL);

	cand->free(field);
	cand->free(array);
	cand->free(last);
	cand->free(large);
	return bad;
}

#define PAIRS 10000000
#define RING 1024
#define THREADS 8

// pairs робить PAIRS пар malloc/free по 16–64 байти: кільце з RING живих
// блоків, кожна алокація заміщує найстаріший.
static void *pairs(void *arg) {
	(void)arg;
	void *ring[RING] = {0};
	for (unsigned i = 0; i < PAIRS; i++) {
		unsigned j = i & (RING - 1);
		if (ring[j] != NULL) {
			cand->free(ring[j]);
		}
		ring[j] = cand->alloc(16 + ((i * 7) & 3) * 16);
		*(volatile char *)ring[j] = (char)i;
	}
	for (unsigned j = 0; j < RING; j++) {
		if (ring[j] != NULL) {
			cand->free(ring[j]);
		}
	}
	return NULL;
}

static double now(void) {
	struct timespec ts;
	clock_gettime(CLOCK_MONOTONIC, &ts);
	return ts.tv_sec + ts.tv_nsec / 1e9;
}

static int run_seq(void) {
	double t = now();
	pairs(NULL);
	printf("seq\t%.3f s\n", now() - t);
	return 0;
}

static int run_par(void) {
	pthread_t th[THREADS];
	double t = now();
	for (int i = 0; i < THREADS; i++) {
		if (pthread_create(&th[i], NULL, pairs, NULL) != 0) {
			return 1;
		}
	}
	for (int i = 0; i < THREADS; i++) {
		pthread_join(th[i], NULL);
	}
	printf("par\t%.3f s\n", now() - t);
	return 0;
}

typedef struct node {
	struct node *left, *right;
	long pad[2];
} node;

static node *build(int depth) {
	node *n = cand->alloc(sizeof(node));
	n->left = n->right = NULL;
	if (depth > 0) {
		n->left = build(depth - 1);
		n->right = build(depth - 1);
	}
	return n;
}

static void release(node *n) {
	if (n->left != NULL) {
		release(n->left);
		release(n->right);
	}
	cand->free(n);
}

static long rss_kib(void) {
	long pages = 0, resident = 0;
	FILE *f = fopen("/proc/self/statm", "r");
	if (f == NULL || fscanf(f, "%ld %ld", &pages, &resident) != 2) {
		resident = -1;
	}
	if (f != NULL) {
		fclose(f);
	}
	return resident * (sysconf(_SC_PAGESIZE) / 1024);
}

// Дерево глибини 20 (2^21-1 вузлів по 32 байти): побудова, звільнення,
// пікова RSS і RSS після звільнення.
static int run_tree(void) {
	double t = now();
	release(build(20));
	double d = now() - t;
	struct rusage ru;
	getrusage(RUSAGE_SELF, &ru);
	printf("tree\t%.3f s\tpeak %ld KiB\tafter %ld KiB\n", d, ru.ru_maxrss, rss_kib());
	return 0;
}

int main(int argc, char **argv) {
	if (argc != 3) {
		fprintf(stderr, "usage: allocbase <candidate> base|seq|par|tree\n");
		return 1;
	}
	for (size_t i = 0; i < sizeof candidates / sizeof candidates[0]; i++) {
		if (strcmp(argv[1], candidates[i].name) == 0) {
			cand = &candidates[i];
		}
	}
	if (cand == NULL) {
		fprintf(stderr, "unknown candidate %s\n", argv[1]);
		return 1;
	}
	cand->init();
	if (strcmp(argv[2], "base") == 0) {
		return run_base();
	}
	if (strcmp(argv[2], "seq") == 0) {
		return run_seq();
	}
	if (strcmp(argv[2], "par") == 0) {
		return run_par();
	}
	if (strcmp(argv[2], "tree") == 0) {
		return run_tree();
	}
	fprintf(stderr, "unknown mode %s\n", argv[2]);
	return 1;
}
