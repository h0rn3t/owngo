#!/usr/bin/env bash
# Обгортка над compare.py (S1.1): mandelbrot, типово N=16000 і 10 прогонів.
# Використання: owngo/bench/compare-mandelbrot.sh [N] [RUNS] [--json FILE]
exec python3 "$(dirname "$0")/compare.py" mandelbrot "${1:-16000}" "${2:-10}" "${@:3}"
