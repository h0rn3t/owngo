# OwnGo — PROGRESS

Журнал сесій: дата, задача з `PLAN.md`, що зроблено, що лишилось.

## 2026-10-29 — P0.1, P0.1a

- Remote `upstream` (goplus/llgo) додано; форк збігався з `upstream/main` (`b2e1d4e84`).
- Розділ OwnGo у `CLAUDE.md`; `PROGRESS.md` створено.
- Upstream вимагає LLVM 22, а на цій машині macOS SDK 27, який LLD 22 не читає, і стоїть лише LLVM 23. Прибрали правило «фаза 0 не змінює код llgo» і портували форк на LLVM 23 (P0.1a):
  - біндинг `xgo-dev/llvm` скопійовано в `owngo/third_party/llvm` і підключено через `replace`; латки описано в `owngo/third_party/llvm/OWNGO.md` (конфіг llvm23, розщеплений `br`, `isSignatureValid`, `AssignGUIDPass` перед LTO-bitcode, обрізання `ConstInt` до ширини типу — останнє лікувало реальний miscompile порівнянь на `-O1`+);
  - `xtool/env/llvm` указує на `llvm@23`;
  - `optlevel.Level.Pipeline`: LLVM 23 прибрав pipeline `Os`/`Oz`, вони йдуть як `O2` з атрибутами `optsize`/`minsize`;
  - тести, що залежали від формату IR LLVM 22 (`f0x…` для float, багаторядковий `module asm`, `wchar_size`), оновлено за наміром тесту.
- Базова лінія — `owngo/BASELINE.md`: 286 пакетів зелені, падіння — оточення (bun замість Node, Apple `ar`) і пробіл wasm SjLj.
- Критерій P0.1 «той самий набір падінь, що й upstream» на цій машині неперевірний: upstream тут не збирається (69 пакетів `[build failed]`). Зафіксовано в `BASELINE.md`.
- Лишилось: wasm `+exception-handling` (не потрібно для нативних цілей OwnGo); перегляд перевірки macOS SDK під LLD 23; нічого не закомічено.
