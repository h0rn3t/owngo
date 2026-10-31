#!/usr/bin/env bash
# Зводить профіль алокацій кожної Go-програми корпусу owngo/bench з місцями
# census, як P0.8. Профіль знімає тест TestProfile програми: він кличе main
# з малим аргументом під -memprofilerate=1. phase0 відносить вибірки до
# категорій v1; вибірки без місця — unmatched. Скрипт звіряє, що сума
# категорій і unmatched дорівнює сумі alloc_space профілю.
# Таблиці Markdown — у stdout: категорії v1, причини unmatched
# (census.UnmatchedReasons), рядок stdlib за місцями v1 і рядки програми
# без місця. Якщо задано OUT, туди лягають профіль і JSON phase0 кожної
# програми.
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

names=()
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

	reasons=$(jq '[.unmatched_v1.Reasons[]?.Bytes] | add // 0' "$out/$name.json")
	unmatched=$(jq '[.weights_v1[]? | select(.Category == "unmatched") | .Bytes] | add // 0' "$out/$name.json")
	if [[ $reasons != "$unmatched" ]]; then
		echo "$name: сума причин $reasons, unmatched $unmatched" >&2
		exit 1
	fi
	echo "$total" >"$out/$name.total"
	names+=("$name")
done

# share друкує частку байтів від загальної суми профілю $total.
share='(if $total == 0 then "-" else ((. * 1000 / $total | round) / 10 | tostring) + "%" end)'
row='"| " + (map(tostring) | join(" | ")) + " |"'

echo "| Програма | Категорія v1 | Місць | Об'єкти | Байти | Частка байтів |"
echo "| --- | --- | ---: | ---: | ---: | ---: |"
for name in "${names[@]}"; do
	jq -r --arg name "$name" --argjson total "$(cat "$out/$name.total")" "
		.weights_v1[] | [\$name, .Category, .Sites, .Objects, .Bytes, (.Bytes | $share)] | $row" "$out/$name.json"
done

echo
echo "Причини unmatched (частка — від профілю програми):"
echo
echo "| Програма | Причина | Об'єкти | Байти | Частка байтів |"
echo "| --- | --- | ---: | ---: | ---: |"
for name in "${names[@]}"; do
	jq -r --arg name "$name" --argjson total "$(cat "$out/$name.total")" "
		.unmatched_v1.Reasons[] | [\$name, .Category, .Objects, .Bytes, (.Bytes | $share)] | $row" "$out/$name.json"
done

echo
echo "Причина stdlib за місцями v1 пакетів кадрів:"
echo
echo "| Програма | Категорія v1 | Місць | Об'єкти | Байти |"
echo "| --- | --- | ---: | ---: | ---: |"
for name in "${names[@]}"; do
	jq -r --arg name "$name" "
		.unmatched_v1.Stdlib[] | [\$name, .Category, .Sites, .Objects, .Bytes] | $row" "$out/$name.json"
done

echo
echo "Причина module — рядки файлів програми без місця census:"
echo
echo "| Програма | Функція | Рядок | Об'єкти | Байти |"
echo "| --- | --- | --- | ---: | ---: |"
for name in "${names[@]}"; do
	jq -r --arg name "$name" --arg bench "$bench/" "
		.unmatched_v1.Lines[] | [\$name, .Func, (.Pos | ltrimstr(\$bench)), .Objects, .Bytes] | $row" "$out/$name.json"
done
