#!/usr/bin/env bash
# Обгортка над compare.py (S1.1): fannkuchredux, типово N=12 і 10 прогонів.
# Використання: owngo/bench/compare-fannkuchredux.sh [N] [RUNS] [--json FILE]
exec python3 "$(dirname "$0")/compare.py" fannkuchredux "${1:-12}" "${2:-10}" "${@:3}"
