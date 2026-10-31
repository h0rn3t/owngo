#!/usr/bin/env bash
# Зводить профіль алокацій кожної Go-програми корпусу owngo/bench з місцями
# census, як P0.8. Профіль знімає тест TestProfile програми: він кличе main
# з малим аргументом під -memprofilerate=1. phase0 відносить вибірки до
# категорій v1; вибірки без місця — unmatched. Скрипт звіряє, що сума
# категорій і unmatched дорівнює сумі alloc_space профілю.
# Таблиця Markdown — у stdout. Якщо задано OUT, туди лягають профіль і JSON
# phase0 кожної програми.
#
# Потрібні: Go, jq.
# Використання: owngo/bench/census.sh [OUT]
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
bench=$root/owngo/bench
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
out=${1:-$work/out}
mkdir -p "$out"

go build -C "$root" -o "$work/phase0" ./owngo/census/cmd/phase0

echo "| Програма | Категорія v1 | Місць | Об'єкти | Байти | Частка байтів |"
echo "| --- | --- | ---: | ---: | ---: | ---: |"
for dir in "$bench"/*/; do
	name=$(basename "$dir")
	# Каталог без файлів Go — програма Rust для порівняння.
	compgen -G "$dir*.go" >/dev/null || continue
	if [[ ! -f $dir/profile_test.go ]]; then
		echo "$name: немає profile_test.go з TestProfile" >&2
		exit 1
	fi
	prof=$out/$name
	mkdir -p "$prof"
	go test -C "$root" -count=1 -run '^TestProfile$' -memprofilerate=1 \
		-memprofile="$prof/mem.out" -o "$work/$name.test" "./owngo/bench/$name" >/dev/null
	"$work/phase0" -dir "$dir" -profdir "$prof" >"$out/$name.json"

	# Сума alloc_space — другий стовпець рядків вибірок pprof -raw.
	total=$(go tool pprof -raw "$prof/mem.out" 2>/dev/null | awk '
		/^Locations/ { exit }
		/^ *[0-9]+( +[0-9]+)*: / { sum += $2 }
		END { print sum + 0 }')
	weighed=$(jq '[.weights_v1[]?.Bytes] | add // 0' "$out/$name.json")
	if [[ $total != "$weighed" ]]; then
		echo "$name: сума категорій і unmatched $weighed, alloc_space профілю $total" >&2
		exit 1
	fi

	jq -r --arg name "$name" --argjson total "$total" '
		.weights_v1[]
		| [$name, .Category, .Sites, .Objects, .Bytes,
			(if $total == 0 then "-" else ((.Bytes * 1000 / $total | round) / 10 | tostring) + "%" end)]
		| "| " + (map(tostring) | join(" | ")) + " |"' "$out/$name.json"
done
