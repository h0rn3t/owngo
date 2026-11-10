#include <pthread.h>

extern void goBlock(int id);

// run_locked тримає м'ютекс з перевіркою власника, поки Go-callback
// блокується на каналі. 0 — callback повернувся на той самий потік і
// м'ютекс відпущено; -1 — інший потік; інакше — код pthread_mutex_unlock.
int run_locked(int id) {
	pthread_mutexattr_t attr;
	pthread_mutex_t mu;
	pthread_mutexattr_init(&attr);
	pthread_mutexattr_settype(&attr, PTHREAD_MUTEX_ERRORCHECK);
	pthread_mutex_init(&mu, &attr);
	pthread_mutex_lock(&mu);
	pthread_t self = pthread_self();
	goBlock(id);
	int same = pthread_equal(self, pthread_self());
	int rc = pthread_mutex_unlock(&mu);
	if (rc == 0 && !same)
		rc = -1;
	if (rc == 0)
		pthread_mutex_destroy(&mu);
	pthread_mutexattr_destroy(&attr);
	return rc;
}
