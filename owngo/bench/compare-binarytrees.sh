#!/usr/bin/env bash
# Обгортка над compare.py (S1.1): binarytrees, типово N=21 і 10 прогонів.
# Використання: owngo/bench/compare-binarytrees.sh [N] [RUNS] [--json FILE]
exec python3 "$(dirname "$0")/compare.py" binarytrees "${1:-21}" "${2:-10}" "${@:3}"
