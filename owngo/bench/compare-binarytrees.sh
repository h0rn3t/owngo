#!/usr/bin/env bash
# Порівнює binary-trees: Go #2 (binarytrees) під go, binarytreesarena під llgo
# з тегом owngo і програму Rust #5 (bumpalo + rayon) з binarytreesrust. Кожна
# запускається RUNS разів на N, вивід звіряється з
# binarytrees/testdata/expected-$N.txt. Окремо — CPU-час (user) Go #2 під owngo
# на 18: на всіх ядрах і під taskset -c 0. Таблиця Markdown — у stdout. Лише
# Linux: ru_maxrss читається як КБ.
#
# Потрібні: Go, LLVM 23 першим у PATH, cargo, python3, taskset.
# Використання: owngo/bench/compare-binarytrees.sh [N] [RUNS]
set -euo pipefail

n=${1:-21}
runs=${2:-3}
root=$(cd "$(dirname "$0")/../.." && pwd)
bench=$root/owngo/bench
expected=$bench/binarytrees/testdata/expected-$n.txt
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

go build -C "$root" -o "$work/llgo" ./cmd/llgo
(cd "$root" && LLGO_ROOT=$root "$work/llgo" build -tags owngo -o "$work/owngo" ./owngo/bench/binarytrees)
(cd "$root" && LLGO_ROOT=$root "$work/llgo" build -tags owngo -o "$work/arena" ./owngo/bench/binarytreesarena)
# Go #2 на сторінці Benchmarks Game зібрано з GOAMD64=v2.
if [[ $(uname -m) == x86_64 ]]; then
	GOAMD64=v2 go build -C "$root" -o "$work/go" ./owngo/bench/binarytrees
else
	go build -C "$root" -o "$work/go" ./owngo/bench/binarytrees
fi
# Прапори сторінки Rust #5: opt-level=3 і codegen-units=1 дає профіль release.
if [[ $(uname -m) == x86_64 ]]; then
	export RUSTFLAGS="-C target-cpu=ivybridge"
fi
cargo build --quiet --release --locked \
	--manifest-path "$bench/binarytreesrust/Cargo.toml" --target-dir "$work/cargo"
cp "$work/cargo/release/binarytrees" "$work/rust"

# measure друкує час у секундах, пікову RSS дочірнього процесу в МБ і його
# CPU-час user у секундах.
measure() {
	python3 -I -c '
import resource, subprocess, sys, time
start = time.monotonic()
with open(sys.argv[1], "wb") as out:
    code = subprocess.run(sys.argv[2:], stdout=out).returncode
elapsed = time.monotonic() - start
usage = resource.getrusage(resource.RUSAGE_CHILDREN)
print(f"{elapsed:.2f} {usage.ru_maxrss // 1024} {usage.ru_utime:.2f}")
sys.exit(code)
' "$@"
}

echo "binary-trees $n, $runs runs, $(nproc) CPUs"
echo
echo "| Program | Times, s | Best, s | User of best, s | Peak RSS, MB | Output |"
echo "| --- | --- | --- | --- | --- | --- |"
for prog in go arena rust; do
	times=()
	rss=0
	best=
	output=matches
	for _ in $(seq "$runs"); do
		read -r t m u < <(measure "$work/out-$prog" "$work/$prog" "$n")
		times+=("$t")
		if [[ -z $best ]] || awk -v a="$t" -v b="$best" 'BEGIN { exit !(a < b) }'; then
			best=$t
			user=$u
		fi
		((m > rss)) && rss=$m
		if [[ -f $expected ]] && ! cmp -s "$work/out-$prog" "$expected"; then
			output=DIFFERS
		fi
	done
	[[ -f $expected ]] || output="no expected-$n.txt"
	case $prog in
	go) label="Go #2, go" ;;
	arena) label="Go arena, llgo -tags owngo" ;;
	rust) label="Rust #5, bumpalo + rayon" ;;
	esac
	echo "| $label | ${times[*]} | $best | $user | $rss | $output |"
	[[ $output == matches || $output == no* ]] || status=1
done

# CPU-час одного прогону Go #2 під owngo на 18: на всіх ядрах і на одному.
read -r t _ all < <(measure /dev/null "$work/owngo" 18)
read -r t1 _ one < <(measure /dev/null taskset -c 0 "$work/owngo" 18)
echo
echo "Go #2, llgo -tags owngo, 18: user $all s ($t s wall) on $(nproc) CPUs, user $one s ($t1 s wall) under taskset -c 0"
exit "${status:-0}"
