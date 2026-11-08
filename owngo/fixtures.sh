#!/usr/bin/env bash
# Запускає кожну програму прийняття owngo/* і звіряє результат з очікуваним.
#
# Використання: owngo/fixtures.sh [LLGO]   (типово — llgo з PATH)
#
# Очікування задає розташування фікстури:
#   owngo/asan/*uaf — під -O0 -asan процес має впасти зі звітом AddressSanitizer;
#   owngo/asan/*    — під -O0 -asan код 0 і жодного звіту;
#   решта           — код 0.
# Тег owngostat додається, якщо його вимагає рядок //go:build фікстури.
# Каталоги bench, census/cmd, third_party, stand (окремі модулі) і пакети site —
# не програми прийняття.
set -uo pipefail

llgo=${1:-llgo}
root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"

# Без GC LeakSanitizer бачить витоком кожен живий об'єкт на виході.
export ASAN_OPTIONS=${ASAN_OPTIONS:-detect_leaks=0}

# Відомі падіння, записані в PROGRESS.md. Фікстура зі списку, що пройшла, —
# теж помилка: її треба прибрати звідси.
declare -A known=(
	[owngo/bytes/imconcat]="freed delta 0 want 8; pre-existing, awaiting a decision (PROGRESS.md)"
	[owngo/counted]="links C.malloc_size, a macOS API absent on Linux"
	[owngo/runtimefree]="links C.malloc_size, a macOS API absent on Linux"
)

failed=()
count=0
while IFS= read -r dir; do
	count=$((count + 1))
	tags=owngo
	grep -q '^//go:build.*owngostat' "$dir"/*.go && tags=owngo,owngostat
	flags=(-tags "$tags")
	case $dir in owngo/asan/*) flags+=(-O0 -asan) ;; esac

	out=$("$llgo" run "${flags[@]}" "./$dir" 2>&1)
	code=$?
	report=no
	grep -q AddressSanitizer <<<"$out" && report=yes

	case $dir in
	owngo/asan/*uaf) [[ $code -ne 0 && $report == yes ]] ;;
	*) [[ $code -eq 0 && $report == no ]] ;;
	esac
	passed=$?
	if [[ -n ${known[$dir]:-} ]]; then
		if [[ $passed -ne 0 ]]; then
			echo "known $dir: ${known[$dir]}"
			continue
		fi
		echo "FAIL $dir passes but is listed as a known failure in owngo/fixtures.sh"
		failed+=("$dir")
		continue
	fi
	if [[ $passed -eq 0 ]]; then
		echo "ok   $dir"
		continue
	fi
	echo "FAIL $dir (exit $code, ASan report: $report)"
	tail -n 30 <<<"$out" | sed 's/^/    /'
	failed+=("$dir")
done < <(find owngo -name main.go \
	-not -path 'owngo/bench/*' -not -path 'owngo/census/cmd/*' \
	-not -path 'owngo/third_party/*' -not -path 'owngo/stand/*' -not -path '*/site/*' |
	xargs -n1 dirname | sort)

echo "${#failed[@]} of $count fixtures failed"
[[ ${#failed[@]} -eq 0 ]]
