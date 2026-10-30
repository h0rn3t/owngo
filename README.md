LLGo - A Go compiler based on LLVM
=====

[![Build Status](https://github.com/xgo-dev/llgo/actions/workflows/go.yml/badge.svg)](https://github.com/xgo-dev/llgo/actions/workflows/go.yml)
[![GitHub release](https://img.shields.io/github/v/tag/xgo-dev/llgo.svg?label=release)](https://github.com/xgo-dev/llgo/releases)
[![Coverage Status](https://codecov.io/gh/xgo-dev/llgo/branch/main/graph/badge.svg)](https://codecov.io/gh/xgo-dev/llgo)
[![LLGo bindings](https://img.shields.io/badge/llarhub-LLGo_Bindings-brightgreen.svg)](https://github.com/xgo-dev/llgo/blob/main/doc/LLGo-Bindings.md)
[![Benchmark](https://img.shields.io/badge/benchmark-LLGo_vs_Go-yellowgreen.svg)](https://llgo-test.github.io/benchmarks/)
[![GoDoc](https://pkg.go.dev/badge/github.com/xgo-dev/llgo.svg)](https://pkg.go.dev/github.com/xgo-dev/llgo)
[![XGo](https://img.shields.io/badge/project-XGo-blue.svg)](https://github.com/goplus/xgo)

LLGo is a Go compiler based on LLVM in order to better integrate Go with the C ecosystem. It's a subproject of [the XGo project](https://github.com/goplus/xgo).

LLGo aims to expand the boundaries of Go/XGo, providing limitless possibilities such as:

* Game development
* AI and data science
* WebAssembly
* Embedded development
* ...

How can these be achieved?

```
LLGo := Go * C ecosystem
```

LLGo is compatible with the C ecosystem through the C **Application Binary Interface (ABI)**, while LLGo is compatible with Go at the **source-code level**. The C ecosystem means C/C++ libraries accessible through the C ABI.


## Go support

LLGo is compatible with Go 1.20+ source code and supports the complete Go 1.27 language syntax, as well as `cgo`.

Compiler compatibility is checked against applicable upstream [`GOROOT/test`](test/goroot/README.md) cases using the pinned Go 1.27 toolchain. User projects and packages under `test/` are additionally tested with exact Go 1.20 through Go 1.27 toolchains. Remaining applicable differences are recorded in [`xfail.yaml`](test/goroot/xfail.yaml); gc-specific mechanisms outside LLGo's compatibility goals are documented in [`notapplicable.yaml`](test/goroot/notapplicable.yaml).

### Runtime

LLGo uses a different runtime from the standard Go toolchain. Native goroutines map 1:1 to OS threads with fixed native stacks, so direct C calls require no Go-to-C stack or scheduler transition, avoiding the cgo overhead that makes frequent C calls costly in standard Go.

The default garbage collector is conservative [BDWGC](https://www.hboehm.info/gc/) (also known as libgc). Bare-metal embedded targets instead use a TinyGo-derived conservative mark-and-sweep collector.

Garbage collection can be disabled with the `nogc` build tag. For example:

```sh
llgo run -tags nogc .
```

### Standard libraries

LLGo fully supports the Go standard library on supported native platforms. CI requires compatibility coverage for every public package and exported symbol in the primary Go toolchain, and runs focused [`test/std`](test/std/README.md) compatibility sets with each older supported toolchain.

Other targets may not provide every OS service or implementation-specific runtime behavior.

| Target | Current coverage |
| --- | --- |
| Native | Linux amd64/arm64 and macOS amd64/arm64 release builds; primary CI on Linux amd64 and macOS arm64 |
| WebAssembly | `js/wasm` and `wasip1/wasm` builds; WASI and Emscripten CI coverage |
| Embedded | [`-target`](doc/Embedded_Cmd.md) configurations for supported boards and MCUs, with selected QEMU/emulator smoke tests |

The supported WebAssembly profiles combine a memory ABI with a host ABI.
`-target emscripten` emits a Memory32 ES module and its sibling wasm module,
`-target emscripten-memory64` emits the corresponding Memory64 pair, and
`-target wasi` emits a Memory32 WASI Preview 1 module. Raw
`GOOS=js GOARCH=wasm` and `GOOS=wasip1 GOARCH=wasm` select the Go-compatible
JavaScript and WASI host contracts. See the [WebAssembly proposal](doc/wasm-proposal.md)
for the compatibility contract and acceptance plan.

WebAssembly goroutine stacks have fixed sizes. For deeply recursive workloads,
set `-goroutine-stack-size=2MB` when building (the existing `-pthread-stack-size`
flag remains an alias). This sets both the goroutine stack and its Asyncify
buffer; it does not change the main goroutine's stack. JavaScript-hosted builds
check stack bounds and abort on overflow instead of corrupting linear memory.
These checks remain enabled in optimized builds: checking each stack-pointer
update adds code and execution overhead, but a return-time cookie check alone
cannot prevent a goroutine from overwriting adjacent memory.


## C/C++ support

LLGo lets you import and call C/C++ libraries directly, without wrappers or cgo overhead.

### Interop mechanism

LLGo uses `go:linkname` to bind a Go declaration directly to a C ABI symbol:

<!-- embedme doc/_readme/llgo_call_c/call_c.go#L3-L6 -->

```go
import _ "unsafe" // for go:linkname

//go:linkname Sqrt C.sqrt
func Sqrt(x float64) float64
```

You can use this directly in your own code:

<!-- embedme doc/_readme/llgo_call_c/call_c.go -->

```go
package main

import _ "unsafe" // for go:linkname

//go:linkname Sqrt C.sqrt
func Sqrt(x float64) float64

func main() {
	println("sqrt(2) =", Sqrt(2))
}
```

Or organize such bindings into a package, as [c/math](https://github.com/goplus/lib/tree/main/c/math) does:

<!-- embedme doc/_readme/llgo_call_cmath/call_cmath.go -->

```go
package main

import "github.com/goplus/lib/c/math"

func main() {
	println("sqrt(2) =", math.Sqrt(2))
}
```

Because calls into C compile to native calls against the C ABI, there is no Go-to-C stack or scheduler transition, so frequent C calls stay cheap.

#### Promoted C variadic methods

Forwarding an incoming C ellipsis through a promoted-method wrapper, including
an interface call, is currently supported only on hosted native targets without
an explicit `-target` profile. The underlying C method must have a pointer
receiver. The wrapper and callee must have matching fixed LLVM prototypes and
calling conventions, with no result conversion. Fixed parameters must be
scalars, with zero or one scalar result: booleans, integers, floating-point
values, and pointers qualify.

Value receivers, aggregate fixed parameters or results, multiple results,
Wasm, and explicit `-target` profiles (including custom native profiles) remain
outside this support. Selecting an unsupported forwarding wrapper through an
interface conversion, method expression, or bound method produces a compiler
error naming the method, target, and reason forwarding is unsupported. Call the C-linked method
or function directly with explicit C-compatible arguments when forwarding is
unavailable. Unused synthetic wrappers do not prevent those direct calls.

#### Calling-convention directives

A function type that models a C callback carries a calling-convention directive so the generated function pointer matches the native ABI:

* `//llgo:cdecl` selects the C calling convention.

```go
import (
	"unsafe"

	"github.com/goplus/lib/c"
)

//llgo:cdecl
type Handler func(data unsafe.Pointer) c.Int
```

A native callback is one function pointer, so a Go callback must be a direct function reference; pass state through an explicit context pointer rather than a capturing closure.

The older `//llgo:type C` spelling remains supported for backward compatibility but is **deprecated**: the compiler accepts it and prints a warning naming the replacement (`//llgo:type C` → `//llgo:cdecl`). Prefer the new directive in new code.

### C/C++ standard libraries

LLGo provides Go bindings for the C/C++ standard library:

| Package | Description |
| --- | --- |
| [c](https://pkg.go.dev/github.com/goplus/lib/c) | C standard library core |
| [c/syscall](https://pkg.go.dev/github.com/goplus/lib/c/syscall) | System calls |
| [c/sys](https://pkg.go.dev/github.com/goplus/lib/c/sys) | System headers |
| [c/os](https://pkg.go.dev/github.com/goplus/lib/c/os) | OS interfaces |
| [c/math](https://pkg.go.dev/github.com/goplus/lib/c/math) | Math functions |
| [c/math/cmplx](https://pkg.go.dev/github.com/goplus/lib/c/math/cmplx) | Complex math |
| [c/math/rand](https://pkg.go.dev/github.com/goplus/lib/c/math/rand) | Random number generation |
| [c/pthread](https://pkg.go.dev/github.com/goplus/lib/c/pthread) | POSIX threads |
| [c/pthread/sync](https://pkg.go.dev/github.com/goplus/lib/c/pthread/sync) | Thread synchronization |
| [c/sync/atomic](https://pkg.go.dev/github.com/goplus/lib/c/sync/atomic) | Atomic operations |
| [c/time](https://pkg.go.dev/github.com/goplus/lib/c/time) | Time functions |
| [c/net](https://pkg.go.dev/github.com/goplus/lib/c/net) | Networking |
| [cpp/std](https://pkg.go.dev/github.com/goplus/lib/cpp/std) | C++ standard library core |

Here is a simple example calling the C `printf` function:

<!-- embedme doc/_readme/llgo_simple/simple.go -->

```go
package main

import "github.com/goplus/lib/c"

func main() {
	c.Printf(c.Str("Hello world\n"))
}
```

`c.Str` is not a runtime conversion from a Go string to a C string — it is a built-in instruction that `llgo` recognizes and compiles directly into a C string constant.

Additional demos are available in the `_demo` directory (prefixed with `_` so the `go` command skips them):

* [hello](_demo/c/hello/main.go): call C `printf` and `fprintf` with Go and C strings
* [qsort](_demo/c/qsort/qsort.go): call a C function that takes a callback (e.g. `qsort`)

To run a demo (see [How to install](#how-to-install) if `llgo` isn't installed yet):

```sh
cd <demo-directory>  # e.g. cd _demo/c/hello
llgo run .
```

### Other frequently used libraries

Beyond the standard library, LLGo can import libraries from across the C/C++ ecosystem. Bindings are currently maintained by hand; automating this process is planned for the future.

Available bindings include:

* [c/bdwgc](https://pkg.go.dev/github.com/goplus/lib/c/bdwgc)
* [c/cjson](https://pkg.go.dev/github.com/goplus/lib/c/cjson)
* [c/clang](https://pkg.go.dev/github.com/goplus/lib/c/clang)
* [c/ffi](https://pkg.go.dev/github.com/goplus/lib/c/ffi)
* [c/libuv](https://pkg.go.dev/github.com/goplus/lib/c/libuv)
* [c/llama2](https://pkg.go.dev/github.com/goplus/lib/c/llama2)
* [c/lua](https://pkg.go.dev/github.com/goplus/lib/c/lua)
* [c/neco](https://pkg.go.dev/github.com/goplus/lib/c/neco)
* [c/openssl](https://pkg.go.dev/github.com/goplus/lib/c/openssl)
* [c/raylib](https://pkg.go.dev/github.com/goplus/lib/c/raylib)
* [c/sqlite](https://pkg.go.dev/github.com/goplus/lib/c/sqlite)
* [c/zlib](https://pkg.go.dev/github.com/goplus/lib/c/zlib)
* [cpp/inih](https://pkg.go.dev/github.com/goplus/lib/cpp/inih)
* [cpp/llvm](https://pkg.go.dev/github.com/goplus/lib/cpp/llvm)

Examples built on these bindings:

* [llama2-c](_demo/c/llama2-c): inference Llama 2 (the first LLGo AI example)
* [mkjson](https://github.com/goplus/lib/tree/main/c/cjson/_demo/mkjson): create a JSON object and print it
* [sqlitedemo](https://github.com/llarhub/sqlite/tree/main/demo/sqlitedemo): a basic SQLite demo
* [tetris](https://github.com/llarhub/raylib/tree/main/demo/tetris): a Tetris game based on raylib


## Dependencies

- [Go 1.27](https://go.dev) for building LLGo; CI validates user packages separately with Go 1.20 through Go 1.27
- [LLVM 22](https://llvm.org)
- [Clang 22](https://clang.llvm.org)
- [LLD 22](https://lld.llvm.org)
- [LLDB](https://lldb.llvm.org) (LLVM 22 packages on Linux; Xcode's Apple LLDB on macOS)
- [pkg-config 0.29+](https://gitlab.freedesktop.org/pkg-config/pkg-config)
- [bdwgc/libgc 8.0+](https://www.hboehm.info/gc/)
- [libffi](https://sourceware.org/libffi/)
- [OpenSSL 3.0+](https://www.openssl.org/)
- [zlib 1.2+](https://github.com/madler/zlib)

## How to install

Use the platform package manager instructions below whenever possible.

### with mise or Pixi (development)

Install [mise](https://mise.jdx.dev/installing-mise.html) or
[Pixi](https://pixi.prefix.dev/latest/installation/), then enter the environment
from the repository root:

```sh
mise -C dev trust
mise -C dev install --locked
mise -C dev en
```

Or, with Pixi:

```sh
cd dev
pixi shell --locked
```

In either shell, run `cd ..`, then use `go run ./cmd/llgo version` or
`go build ./cmd/llgo`. See [development tooling](dev/README.md) for details.

### on macOS

<!-- embedme doc/_readme/scripts/install_macos.sh#L2-L1000 -->

```sh
brew update
brew install llvm@23 lld@23 bdw-gc openssl cjson libffi libuv pkg-config
brew link --force --overwrite llvm@23 lld@23 libffi
./install.sh
```

Native macOS builds use LLVM/LLD 23 and require a macOS SDK older than 27.
LLGo keeps the active SDK when it meets that requirement; otherwise it selects
the newest installed older SDK from Xcode or Command Line Tools. SDK 27 and
newer are excluded. Set `SDKROOT` to pin an installed older SDK; an unsupported
or missing explicit SDK is an error. If no older SDK is installed, install one
before building. The selected SDK is used for both compilation and linking.

Homebrew's versioned LLVM 23 formula does not ship LLDB and there is no `lldb@23` formula. LLGo checks common Homebrew and system locations, then `lldb` on `PATH`; use `LLGO_LLDB` or `llgo lldb -lldb` to select one explicitly.

### on Linux

The APT/YUM configurations below accept packages without GPG signature verification
(`trusted=yes` / `gpgcheck=0`); downloads use HTTPS.

#### Debian/Ubuntu

Install the latest stable release for amd64 or arm64 from the APT repository:

```sh
echo 'deb [trusted=yes] https://pkgs.goplus.org/apt/ /' | sudo tee /etc/apt/sources.list.d/goplus.list
sudo apt-get update
sudo apt-get install -y llgo
llgo version
```

For source builds, install the build dependencies:

<!-- embedme doc/_readme/scripts/install_ubuntu.sh#L2-L1000 -->

```sh
echo "deb http://apt.llvm.org/$(lsb_release -cs)/ llvm-toolchain-$(lsb_release -cs)-23 main" | sudo tee /etc/apt/sources.list.d/llvm.list
wget -O - https://apt.llvm.org/llvm-snapshot.gpg.key | sudo apt-key add -
sudo apt-get update
sudo apt-get install -y llvm-23-dev clang-23 libclang-23-dev lld-23 lldb-23 libunwind-23-dev libc++-23-dev pkg-config libgc-dev libssl-dev zlib1g-dev libffi-dev libcjson-dev libsqlite3-dev libuv1-dev
./install.sh
```

#### Alpine Linux

```sh
apk add go llvm22-dev clang22-dev lld22 lldb pkgconf gc-dev libunwind-dev openssl-dev zlib-dev libffi-dev cjson-dev sqlite-dev libuv-dev
apk add g++ # build only
export LLVM_CONFIG=/usr/lib/llvm22/bin/llvm-config
export CGO_CPPFLAGS="$($LLVM_CONFIG --cppflags)"
export CGO_CXXFLAGS=-std=c++17
export CGO_LDFLAGS="$($LLVM_CONFIG --ldflags) $($LLVM_CONFIG --libs all)"
./install.sh
```

#### Fedora Linux 44 or newer

Install the latest stable release for amd64 or arm64 from the YUM/DNF repository:

```sh
sudo tee /etc/yum.repos.d/fury.repo > /dev/null <<'EOF'
[fury]
name=goplus (Gemfury)
baseurl=https://pkgs.goplus.org/yum/
enabled=1
gpgcheck=0
EOF
sudo dnf install -y llgo
llgo version
```

With YUM, use `sudo yum install -y llgo` for the installation command.

For source builds, Fedora 44 and 45 ship LLVM 22 as the default LLVM stack.
Fedora 43 still ships LLVM 21 and is not a supported default-toolchain
environment for this LLGo release.

```sh
sudo dnf install -y llvm-devel clang-devel lld lldb libcxx-devel llvm-libunwind-devel \
  pkgconf-pkg-config gc-devel openssl-devel libffi-devel libuv-devel \
  cjson-devel sqlite-devel zlib-ng-compat-devel
llvm-config --version # must report 22.x
./install.sh
```

docker alpine 386 llgo environment
```
export GCC_ROOT_DIR=$(gcc -print-search-dirs | grep 'install:' | awk -F': ' '{print $2}')
export LDFLAGS="-L$GCC_ROOT_DIR -B$GCC_ROOT_DIR -Wl,-dynamic-linker,/lib/ld-musl-i386.so.1"
llgo run .
```

### Install from source

<!-- embedme doc/_readme/scripts/install_llgo.sh#L2-L1000 -->

```sh
git clone https://github.com/xgo-dev/llgo.git
cd llgo
./install.sh
```

### Fallback release installers

The Unix fallback installs LLGo and, by default, Go and native C libraries. It
does not install LLVM/Clang/LLD 22; install those with the platform steps above
before compiling programs with LLGo.

On macOS or Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/xgo-dev/llgo/main/install.sh | bash
```


## Go environment

Use `llgo env GOOS GOARCH` to query Go environment values and `llgo env LLGO_ROOT LLGO_RUNTIME_DIR LLGO_LLVM_VERSION` to inspect the LLGo installation and selected LLVM toolchain. With no variable names it prints both sets. `-json` provides machine-readable output, and `-target name` adds the resolved `LLGO_TARGET_*` fields for an embedded target without downloading tools or building libraries. For `LLGO_*` fields, `-changed` includes only values explicitly set in the process environment and omits derived values; Go fields retain the underlying `go env -changed` behavior. Paths for optional packaged tools are empty when the payload is not installed.

Go-only queries are delegated to the underlying Go toolchain and respect its environment and configuration. The `-w` and `-u` flags update the same GOENV configuration as `go env`; LLGo fields are read-only.

A real Go executable must remain in an absolute PATH directory. If `go` is a symlink or hard link to `llgo`, the environment command skips that link and searches the remaining PATH entries instead of invoking itself recursively.

## Module management

Use `llgo mod init`, `llgo mod tidy`, `llgo mod vendor`, and other `go mod` subcommands to manage modules with the underlying Go toolchain. Arguments, working directory, environment, standard streams, and exit status are preserved. As with `llgo env`, a real Go executable must remain in an absolute PATH directory; links from `go` back to LLGo are skipped to prevent recursion.

Module maintenance does not inject LLGo-specific build tags. Go's `tidy` and `vendor` already consider all build tags except `ignore`, including dependencies imported only by files with `//go:build llgo`.

## Development tools

`llgo list` provides a Go-compatible package query backed by the real Go toolchain while adding LLGo's source-selection build tags to package queries. Module queries keep caller-supplied and explicit target tags but do not add LLGo's defaults. It supports the list queries used by `golang.org/x/tools/go/packages`, including `list -f '{{context.GOARCH}} {{context.Compiler}}' -- unsafe`; the reported `gc` context identifies Go frontend and type-size compatibility, not LLGo's LLVM backend. Use `-target name` to apply a resolved LLGo target's GOOS, GOARCH, and build tags without installing its toolchain. When `go/packages` requests `-export=true`, paths in the `Export` field refer to Go frontend type data in Go's build cache, not LLGo package archives.

`llgo get`, `llgo fix`, `llgo generate`, and `llgo vet` use the Go toolchain with LLGo's build tags and accept `-target` to select the target GOOS, GOARCH, and tags. `llgo fmt` and `llgo work` forward arguments unchanged; `go fmt` already includes files excluded by build constraints.

`build`, `clean`, `install`, `run`, `test`, `version`, and `tool compile` remain LLGo-specific. The Go `bug`, `doc`, and `telemetry` commands are not exposed: the first and last operate Go services, while `go doc` cannot select LLGo-only sources because it has no build-tag option.

* [llgen](chore/llgen): It is used to compile Go packages into LLVM IR files (*.ll).
* [gentests](chore/gentests): It refreshes runtime-output and package-metadata golden data under `cl/_test*`. LLVM IR checks live in Go sources as `// LITTEST` FileCheck directives.
* [litgen](chore/litgen): It maintains explicitly opted-in, source-embedded FileCheck snapshots. It supports function/global selection, update-only operation, stale-check verification, and stable LLVM value abstractions. Small handwritten checks remain manual.
* [ssadump](chore/ssadump): It is a Go SSA builder and interpreter.

For local workflows and test-golden refresh commands, see [dev/README.md](dev/README.md#6-refresh-test-goldens).

How do I generate these tools?

<!-- embedme doc/_readme/scripts/install_full.sh#L2-L1000 -->

```sh
git clone https://github.com/xgo-dev/llgo.git
cd llgo
go install -v ./cmd/...
go install -v ./chore/...
```

## Key modules

Below are the key modules for understanding the implementation principles of `llgo`:

* [ssa](https://pkg.go.dev/github.com/xgo-dev/llgo/ssa): It generates LLVM IR files (LLVM SSA) using the semantics and interfaces of Go SSA. Although `LLVM SSA` and `Go SSA` are both IR languages, they work at completely different levels. `LLVM SSA` is closer to machine code and abstracts over different instruction sets, while `Go SSA` is closer to a high-level language. We can think of it as the instruction set of the `Go computer`. `llgo/ssa` is not limited to the `llgo` compiler. If we view it as providing the high-level expressive power of `LLVM`, it is very useful. Its advanced SSA form lets clients use LLVM without operating directly on machine-code semantics.
* [cl](https://pkg.go.dev/github.com/xgo-dev/llgo/cl): It is the core of the llgo compiler. It converts a Go package into LLVM IR files. It depends on `llgo/ssa`.
* [internal/build](https://pkg.go.dev/github.com/xgo-dev/llgo/internal/build): It strings together the entire compilation process of `llgo`. It depends on `llgo/ssa` and `llgo/cl`.
