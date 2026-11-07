// Семплер CPU через LD_PRELOAD для машин без perf (perf_event_paranoid = 4).
// setitimer(ITIMER_PROF) раз на 1 мс CPU процесу шле SIGPROF; обробник
// записує адресу інструкції потоку, що виконувався. Час ядра (сторінкові
// збої, madvise) теж іде в ITIMER_PROF і приписується адресі, де потік увійшов
// у ядро. На виході файл $OWNGO_SAMPLER_OUT отримує /proc/self/maps і
// адреси; sampler.py зводить їх у функції через llvm-symbolizer.
//
//   clang -O2 -shared -fPIC -o sampler.so sampler.c
//   OWNGO_SAMPLER_OUT=out.txt LD_PRELOAD=./sampler.so ./prog
#define _GNU_SOURCE
#include <signal.h>
#include <stdatomic.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/time.h>
#include <ucontext.h>

#define MAX_SAMPLES (1 << 22)

static uintptr_t samples[MAX_SAMPLES];
static atomic_size_t nsamples;

static void on_prof(int sig, siginfo_t *si, void *uc) {
	(void)sig;
	(void)si;
	size_t i = atomic_fetch_add_explicit(&nsamples, 1, memory_order_relaxed);
	if (i < MAX_SAMPLES)
		samples[i] = (uintptr_t)((ucontext_t *)uc)->uc_mcontext.gregs[REG_RIP];
}

__attribute__((destructor)) static void sampler_dump(void) {
	const char *path = getenv("OWNGO_SAMPLER_OUT");
	if (!path)
		return;
	struct itimerval off = {0};
	setitimer(ITIMER_PROF, &off, NULL);
	FILE *out = fopen(path, "w");
	if (!out)
		return;
	FILE *maps = fopen("/proc/self/maps", "r");
	char line[4096];
	while (maps && fgets(line, sizeof line, maps))
		fprintf(out, "map %s", line);
	if (maps)
		fclose(maps);
	size_t n = atomic_load(&nsamples);
	if (n > MAX_SAMPLES)
		n = MAX_SAMPLES;
	for (size_t i = 0; i < n; i++)
		fprintf(out, "pc %lx\n", (unsigned long)samples[i]);
	fclose(out);
}

__attribute__((constructor)) static void sampler_start(void) {
	if (!getenv("OWNGO_SAMPLER_OUT"))
		return;
	struct sigaction sa = {0};
	sa.sa_sigaction = on_prof;
	sa.sa_flags = SA_SIGINFO | SA_RESTART;
	sigemptyset(&sa.sa_mask);
	sigaction(SIGPROF, &sa, NULL);
	struct itimerval it = {{0, 1000}, {0, 1000}};
	setitimer(ITIMER_PROF, &it, NULL);
}
