#!/usr/bin/env bash

set -euo pipefail

repository="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
temporary="$(mktemp -d "${TMPDIR:-/tmp}/llgo-local-install-test.XXXXXXXX")"
temporary="$(cd "$temporary" && pwd)"
trap 'rm -rf "$temporary"' EXIT
checkout="$temporary/local checkout"
mkdir -p "$checkout"
cp "$repository/install.sh" "$checkout/install.sh"

# Exercise the actual entrypoint without rebuilding the compiler.
go() {
    [[ "$PWD" == "$LLGO_INSTALLER_TEST_CHECKOUT" ]] || return 1
    [[ "$*" == 'install ./cmd/llgo' ]] || return 1
    printf 'local go install\n'
}
uname() {
    printf 'Linux\n'
}
export -f go uname
export LLGO_INSTALLER_TEST_CHECKOUT="$checkout"
export LLGO_INSTALLER_LIBRARY_ONLY=0
unset LLGO_ARCHIVE_PATH

expected_root="$checkout"

for line_ending in '\n' '\r\n'; do
    printf 'module github.com/xgo-dev/llgo%b' "$line_ending" >"$checkout/go.mod"
    export GITHUB_ENV="$temporary/github-env"
    : >"$GITHUB_ENV"
    # Calling from elsewhere must still build the checkout beside the script.
    output="$(cd "$temporary" && bash "$checkout/install.sh")"
    [[ "$output" == *'local go install'* ]] || {
        printf 'local installation was not selected for %s\n' "$line_ending" >&2
        exit 1
    }
    [[ "$(<"$GITHUB_ENV")" == "LLGO_ROOT=$expected_root" ]] || {
        printf 'LLGO_ROOT did not identify the native checkout path\n' >&2
        exit 1
    }
done

printf 'local install tests passed (LF and CRLF)\n'
