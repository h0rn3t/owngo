#!/usr/bin/env bash
# measure.sh VARIANT PORT: fanout і навантаження на одному бінарнику стенду.
# Бінарники fiberdb-VARIANT і load шукаються в BIN (типово — поруч зі скриптом).
# FANOUT — список n для /fanout (типово 1000 5000 20000).
set -u
dir=${BIN:-$(dirname "$0")}; v=$1; port=$2
db=$(mktemp -d)/items.db
"$dir/fiberdb-$v" -addr 127.0.0.1:$port -db "$db" 2>"$dir/$v.stderr" &
pid=$!
for _ in $(seq 50); do curl -s -o /dev/null http://127.0.0.1:$port/items && break; sleep 0.1; done
curl -s -o /dev/null -X POST -H 'Content-Type: application/json' -d '{"name":"a","qty":1}' http://127.0.0.1:$port/items
for n in ${FANOUT:-1000 5000 20000}; do
	t0=$(date +%s.%N)
	out=$(curl -s -m 120 -w ' http=%{http_code}' "http://127.0.0.1:$port/fanout?n=$n")
	t1=$(date +%s.%N)
	alive=$(kill -0 $pid 2>/dev/null && echo alive || echo dead)
	echo "$v fanout n=$n: $out time=$(awk "BEGIN{printf \"%.2f\", $t1-$t0}")s threads=$(ls /proc/$pid/task 2>/dev/null | wc -l) server=$alive"
	[ $alive = dead ] && break
done
if kill -0 $pid 2>/dev/null; then
	for c in 100 1000; do echo "$v load c=$c: $("$dir/load" -url http://127.0.0.1:$port/items/1 -c $c -d 5s 2>&1)"; done
	echo "$v VmHWM: $(grep VmHWM /proc/$pid/status)"
fi
kill -INT $pid 2>/dev/null; wait $pid; echo "$v exit=$?"; tail -3 "$dir/$v.stderr"
