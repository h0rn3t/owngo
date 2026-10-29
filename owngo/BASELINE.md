# OwnGo — базова лінія тестів

Зафіксовано 2026-10-29 для P0.1. Зміна не повинна додавати падінь до цього списку.

## Оточення

- macOS 27.2 (SDK 27.0), arm64; Go 1.27.3.
- LLVM 23.1.4 і LLD 23.1.4 з Homebrew (`llvm`, `lld`), bdw-gc 8.2.12.
- Біндинг `github.com/xgo-dev/llvm` — локальна копія під LLVM 23,
  `owngo/third_party/llvm` (див. `OWNGO.md` там).

Команда:

```bash
export LLGO_ROOT=$PWD
export PATH=/opt/homebrew/opt/llvm/bin:/opt/homebrew/opt/lld/bin:$PATH
go test ./...
```

LLVM 23 має бути першим у `PATH`: інакше `clang` береться з `/usr/bin`
(Apple clang 21) і не розбирає IR від LLVM 23.

## Порівняння з upstream

Upstream llgo вимагає LLVM 22, а на macOS SDK 27 явно відмовляється
працювати з LLD 22 (`internal/crosscompile/macos_sdk.go`). Тому на цій машині
upstream не збирається взагалі: перший прогін на `b2e1d4e84` без змін дав 69
пакетів з `[build failed]` (`llvm-c/Core.h` не знайдено). Базова лінія нижче —
для форку після порту на LLVM 23.

## Результат

286 пакетів `ok`, 42 без тестів, 3 падають.

| Пакет | Тест | Причина |
| --- | --- | --- |
| `cmd/internal/browser` | `TestExtensionJavaScript` | оточення: `node` у `PATH` — shim від bun (`~/.bun/bin/node`), потрібен справжній Node.js |
| `internal/targets` | `TestEmscriptenRunnersConsumeExitStatus/emscripten-memory64-runner.mjs` | оточення: той самий shim, немає WebAssembly Memory64 |
| `internal/targets` | `TestEmscriptenRunnersForwardProgramArguments/emscripten-memory64-runner.mjs` | оточення: те саме |
| `internal/build` | — | таймаут 10 хв `go test` під навантаженням повного прогону; окремо з `-timeout 30m` — див. нижче |
