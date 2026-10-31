#!/usr/bin/env bash
# Порівнює binary-trees: binarytreesarena, зібраний llgo з тегом owngo, і
# програму Rust (bumpalo + rayon) з binarytreesrust. Кожна запускається RUNS
# разів на N, вивід звіряється з binarytrees/testdata/expected-$N.txt.
# Таблиця Markdown — у stdout. Лише Linux: ru_maxrss читається як КБ.
#
# Потрібні: Go, LLVM 23 першим у PATH, cargo, python3.
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
(cd "$root" && LLGO_ROOT=$root "$work/llgo" build -tags owngo -o "$work/owngo" ./owngo/bench/binarytreesarena)
# Прапори сторінки Rust #5: opt-level=3 і codegen-units=1 дає профіль release.
if [[ $(uname -m) == x86_64 ]]; then
	export RUSTFLAGS="-C target-cpu=ivybridge"
fi
cargo build --quiet --release --locked \
	--manifest-path "$bench/binarytreesrust/Cargo.toml" --target-dir "$work/cargo"
cp "$work/cargo/release/binarytrees" "$work/rust"

# measure друкує час у секундах і пікову RSS дочірнього процесу в МБ.
measure() {
	python3 -I -c '
import resource, subprocess, sys, time
start = time.monotonic()
with open(sys.argv[1], "wb") as out:
    code = subprocess.run(sys.argv[2:], stdout=out).returncode
elapsed = time.monotonic() - start
print(f"{elapsed:.2f} {resource.getrusage(resource.RUSAGE_CHILDREN).ru_maxrss // 1024}")
sys.exit(code)
' "$@"
}

echo "binary-trees $n, $runs runs, $(nproc) CPUs"
echo
echo "| Program | Times, s | Best, s | Peak RSS, MB | Output |"
echo "| --- | --- | --- | --- | --- |"
for prog in owngo rust; do
	times=()
	rss=0
	output=matches
	for _ in $(seq "$runs"); do
		read -r t m < <(measure "$work/out-$prog" "$work/$prog" "$n")
		times+=("$t")
		((m > rss)) && rss=$m
		if [[ -f $expected ]] && ! cmp -s "$work/out-$prog" "$expected"; then
			output=DIFFERS
		fi
	done
	[[ -f $expected ]] || output="no expected-$n.txt"
	best=$(printf '%s\n' "${times[@]}" | sort -n | head -1)
	label=$([[ $prog == owngo ]] && echo "Go arena, llgo -tags owngo" || echo "Rust bumpalo + rayon")
	echo "| $label | ${times[*]} | $best | $rss | $output |"
	[[ $output == matches || $output == no* ]] || status=1
done
exit "${status:-0}"
