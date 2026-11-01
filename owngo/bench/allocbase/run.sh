#!/usr/bin/env bash
# Міряє кандидатів алокатора фази 8 (P8.2): libc malloc (відлік без бази),
# BDWGC без збирання, алокатор owngo (P8.3, runtime/internal/runtime/_wrap/
# owngo_alloc.c) і mimalloc 3.0.1.
# Окрема збірка з -fsanitize=address перевіряє базу через
# __sanitizer_get_allocated_begin. Кожен замір — окремий процес, найкращий
# із RUNS. Таблиця Markdown — у stdout. Код 1 — неправильна база.
#
# mimalloc — системний libmimalloc-dev (Debian 13, 3.0.1): публічного API
# бази в ньому немає, база — «немає». MIMALLOC_SRC=<розпаковане джерело
# 3.0.1> збирає mimalloc з латкою mimalloc-base.patch (mi_block_base).
#
# Потрібні: LLVM 23 першим у PATH, libgc-dev, libmimalloc-dev. Лише Linux.
# Використання: [MIMALLOC_SRC=<dir>] owngo/bench/allocbase/run.sh [RUNS]
set -euo pipefail

runs=${1:-3}
dir=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

if [[ -n ${MIMALLOC_SRC:-} ]]; then
	cp -r "$MIMALLOC_SRC" "$work/mi"
	patch -s -d "$work/mi" -p1 <"$dir/mimalloc-base.patch"
	# Без -DMI_MALLOC_OVERRIDE: mimalloc не підміняє malloc.
	clang -O2 -DNDEBUG -I"$work/mi/include" -c "$work/mi/src/static.c" -o "$work/mimalloc.o"
	mi=(-DALLOCBASE_MI_BASE -I"$work/mi/include" "$work/mimalloc.o")
	mi_name="mimalloc (латка)"
else
	mi=(-lmimalloc)
	mi_name="mimalloc"
fi

# Системний libmimalloc підміняє malloc, тож mimalloc — окремий бінарник.
cflags=(-O2 -pthread "$dir/main.c" "$dir/../../../runtime/internal/runtime/_wrap/owngo_alloc.c" -lgc)
clang "${cflags[@]}" -o "$work/allocbase"
clang -DALLOCBASE_MIMALLOC "${cflags[@]}" "${mi[@]}" -o "$work/allocbase-mimalloc"
clang -DALLOCBASE_ASAN -fsanitize=address "${cflags[@]}" -o "$work/allocbase-asan"

status=0

# bin друкує бінарник кандидата.
bin() {
	if [[ $1 == mimalloc ]]; then
		echo "$work/allocbase-mimalloc"
	else
		echo "$work/allocbase"
	fi
}

# best друкує поля найшвидшого з RUNS запусків режиму, без назви режиму.
best() {
	for _ in $(seq "$runs"); do "$(bin "$1")" "$1" "$2"; done |
		sort -t$'\t' -k2,2g | head -1 | cut -f2-
}

# base друкує «так», «немає» або «ні»; подробиці «ні» — у stderr.
base() {
	local out
	if ! out=$("$1" "$2" base); then
		echo "$out" >&2
		status=1
		echo "ні"
	elif grep -q none <<<"$out"; then
		echo "немає"
	else
		echo "так"
	fi
}

echo "| Кандидат | База | Пари, 1 потік | Пари, 8 потоків | Дерево 20 | Пікова RSS дерева | RSS після звільнення |"
echo "| --- | --- | ---: | ---: | ---: | ---: | ---: |"
for cand in libc bdwgc owngo mimalloc; do
	name=$cand
	[[ $cand == mimalloc ]] && name=$mi_name
	b=$(base "$(bin "$cand")" "$cand")
	seq=$(best "$cand" seq)
	par=$(best "$cand" par)
	IFS=$'\t' read -r tt peak after <<<"$(best "$cand" tree)"
	echo "| $name | $b | $seq | $par | $tt | ${peak#peak } | ${after#after } |"
done
echo
echo "ASan (\`__sanitizer_get_allocated_begin\`): база — $(base "$work/allocbase-asan" asan)"
exit "$status"
