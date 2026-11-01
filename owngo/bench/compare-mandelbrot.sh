#!/usr/bin/env bash
# Порівнює mandelbrot: порт Rust #4 з mandelbrot, зібраний llgo з тегом
# owngo (типовий -Os і -O2 з повною LTO), llgo без тега і go, з самою
# програмою Rust #4 з mandelbrotrust. Кожна запускається RUNS разів на N,
# SHA-256 виводу звіряється з mandelbrot/testdata/expected-$N.sha256.
# Таблиця Markdown — у stdout. Лише Linux x86_64: ru_maxrss читається як КБ.
#
# Потрібні: Go, LLVM 23 першим у PATH, cargo, python3.
# Використання: owngo/bench/compare-mandelbrot.sh [N] [RUNS]
set -euo pipefail

n=${1:-16000}
runs=${2:-3}
root=$(cd "$(dirname "$0")/../.." && pwd)
bench=$root/owngo/bench
expected=$bench/mandelbrot/testdata/expected-$n.sha256
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

go build -C "$root" -o "$work/llgo" ./cmd/llgo
pkg=./owngo/bench/mandelbrot
(
	cd "$root"
	export LLGO_ROOT=$root
	"$work/llgo" build -tags owngo -o "$work/owngo" "$pkg"
	"$work/llgo" build -tags owngo -O2 -lto full -o "$work/owngo-lto" "$pkg"
	"$work/llgo" build -o "$work/llgo-bin" "$pkg"
	GOAMD64=v2 go build -o "$work/go" "$pkg"
)
# Прапори сторінки Rust #4: opt-level=3 і codegen-units=1 дає профіль release.
RUSTFLAGS="-C target-cpu=ivybridge" cargo build --quiet --release --locked \
	--manifest-path "$bench/mandelbrotrust/Cargo.toml" --target-dir "$work/cargo"
cp "$work/cargo/release/mandelbrot" "$work/rust"

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

echo "mandelbrot $n, $runs runs, $(nproc) CPUs"
echo
echo "| Program | Times, s | Best, s | Peak RSS, MB | Output |"
echo "| --- | --- | --- | --- | --- |"
for prog in owngo owngo-lto llgo-bin go rust; do
	times=()
	rss=0
	output=matches
	for _ in $(seq "$runs"); do
		read -r t m < <(measure "$work/out-$prog" "$work/$prog" "$n")
		times+=("$t")
		((m > rss)) && rss=$m
		if [[ -f $expected ]] &&
			[[ $(sha256sum <"$work/out-$prog" | cut -d' ' -f1) != $(<"$expected") ]]; then
			output=DIFFERS
		fi
	done
	[[ -f $expected ]] || output="no expected-$n.sha256"
	best=$(printf '%s\n' "${times[@]}" | sort -n | head -1)
	case $prog in
	owngo) label="Go port, llgo -tags owngo" ;;
	owngo-lto) label="Go port, llgo -tags owngo -O2 -lto full" ;;
	llgo-bin) label="Go port, llgo" ;;
	go) label="Go port, GOAMD64=v2 go" ;;
	rust) label="Rust #4 (rayon, AVX)" ;;
	esac
	echo "| $label | ${times[*]} | $best | $rss | $output |"
	[[ $output == matches || $output == no* ]] || status=1
done
exit "${status:-0}"
