// rusage запускає команду з stdout у файл і друкує її wall-час, CPU user і
// sys у секундах, пікову RSS у КБ і код виходу. Окремий малий процес, бо
// Linux переносить ru_maxrss процесу через fork і exec: дитина Python
// успадкувала б пікову RSS інтерпретатора.
//
// Використання: rusage OUT CMD [ARG...]
#include <fcntl.h>
#include <stdio.h>
#include <sys/resource.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

static double seconds(struct timeval tv) { return tv.tv_sec + tv.tv_usec / 1e6; }

int main(int argc, char **argv) {
	if (argc < 3) {
		fprintf(stderr, "usage: rusage OUT CMD [ARG...]\n");
		return 2;
	}
	int out = open(argv[1], O_WRONLY | O_CREAT | O_TRUNC, 0644);
	if (out < 0) {
		perror(argv[1]);
		return 2;
	}
	struct timespec start, end;
	clock_gettime(CLOCK_MONOTONIC, &start);
	pid_t pid = fork();
	if (pid < 0) {
		perror("fork");
		return 2;
	}
	if (pid == 0) {
		dup2(out, 1);
		close(out);
		execvp(argv[2], argv + 2);
		perror(argv[2]);
		_exit(127);
	}
	int status;
	struct rusage ru;
	if (wait4(pid, &status, 0, &ru) < 0) {
		perror("wait4");
		return 2;
	}
	clock_gettime(CLOCK_MONOTONIC, &end);
	double wall = (end.tv_sec - start.tv_sec) + (end.tv_nsec - start.tv_nsec) / 1e9;
	int code = WIFEXITED(status) ? WEXITSTATUS(status) : 128 + WTERMSIG(status);
	printf("%.3f %.3f %.3f %ld %d\n", wall, seconds(ru.ru_utime), seconds(ru.ru_stime),
	       ru.ru_maxrss, code);
	return 0;
}
