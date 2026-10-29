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

## 2026-10-29 — видалення Python-інтеропу (поза PLAN.md)

- За рішенням власника прибрано Python-інтероп llgo; `INTENT.md` оновлено (свідомий виняток із мінімального дифу).
- Ядро: `ssa/python.go`, гачки в `Call`/`Load`/`DIGlobal`, `PyDecl`, `vkPy*`, `AfterInit`; у `cl` — `PkgPyModule`, `pyFunc`/`pyVar`, `pystr`/`pyList`/`pyTuple`, імпорт модуля в `init`, третє значення `PyObjRef` у `compileFuncDecl`/`compileFunction`/`funcOf`; у `internal/build` — `Py_Initialize`/`Py_Finalize`, `NeedPyInit` у метаданих кешу, `TestPythonPackage`.
- Видалено `cl/_testpy`, `chore/llpyg`, `_xtool/pydump`, `_demo/py`, Python-приклади `doc/_readme`, образ `pydeps`; README, `doc/LLGo-Bindings.md`, `CLAUDE.md`, CI-установку numpy/torch очищено.
- Лишено: `_demo/c/cgofull` (libpython через звичайний cgo), Python-плагіни gdb/lldb, CI-скрипти на Python.
- Код із `py.`-linkname або `LLGoPackage = "py.…"` тепер падає на лінкуванні (undefined symbol), окремої діагностики немає.
- `go test ./...`: нових падінь немає після виправлення `main_module_test.go`. `internal/build` має два падіння, яких немає в таблиці `owngo/BASELINE.md`, але вони відтворюються на чистому `main` (`6b88530ec`): `TestCreatePackageArchiveFileWithExternalArchiver`, `TestWasmStackAddressesCodegen`.

## 2026-10-29 — P0.2

- Корпус `owngo/census/testdata/src/corpus`: фікстури `analysistest` для `stack`, `static`, `rc-local`, `rc-shared`, `cyclic` (строгий режим без полів-інтерфейсів), `pinned` і `implicit`. Щонайменше п'ять місць на категорію, включно з причинами return, closure, channel, global, go, call і unsafe, внутрішніми вказівниками, `append` з аліасом, викликом через інтерфейс і func-значення, method value, перезаписом глобальної змінної, каналом і `go`.
- Контракт діагностики зафіксовано в коментарі `TestCorpus`: одне місце — один якірний `// want` (`^static$`, `^rc-local: return$`, `^rc-shared: go$`, `^cyclic: return$`, `^pinned: unsafe$`, `^implicit: append$`). Захоплення вказівника замиканням і тимчасовий масив varargs у `append` — окремі місця на тому самому рядку.
- Аналізатора ще немає. `TestCorpus` тримає локальну заглушку, яка нічого не звітує; `census.Analyzer` — задача P0.3.
- Прийняття: `gofmt -l owngo/census` порожньо, `go vet ./owngo/census/` без зауважень. `go test ./owngo/census/...` компілюється і падає з `no diagnostic was reported matching` на всіх 73 очікуваннях.
- Лишилось: P0.3, каркас, що звітує `unknown` і збігається з корпусом за кількістю місць.

## 2026-10-29 — P0.3

- `census.Analyzer` на `buildssa` знаходить місця після lift: кожен `*ssa.Alloc`, `MakeSlice`/`MakeMap`/`MakeChan`, `MakeClosure` зі зв'язками, `MakeInterface` для не-вказівникового типу, виклик builtin `append`, додавання рядків, `Convert` між `string` і `[]byte` або `[]rune`. Кожне місце звітує `unknown`.
- Позиція — `Pos()` інструкції. Якщо у `MakeClosure` літерала функції вона невалідна, береться `Pos` функції SSA (ключове слово `func`).
- Прийняття: `gofmt -l owngo/census` порожньо, `go vet ./owngo/census/` без зауважень. `go test ./owngo/census/ -run TestSiteCount -count=1` зелений: 73 діагностики, по рядках збігаються з `// want`.
- `TestCorpus` лишається червоним: текст `unknown` не збігається з категоріями фікстур. Це задача P0.4.
- Лишилось: P0.4, класифікатор v0, після якого `go test ./owngo/census/...` зелений.

## 2026-10-29 — P0.4

- Класифікатор v0 у `messages`: у межах однієї функції, без розбору полів. Каскад: `pinned`, `stack` (`Alloc` з `Heap == false`), `cyclic` (строгий граф типів, без ребер через інтерфейс і канал), `static`, `rc-shared` (`go`, `channel`, `global`), `rc-local` (`closure`, `return`, `call`). `implicit` не входить у каскад.
- Запис у об'єкт наслідує його втечу. Замикання дає щонайменше `closure`; `go`, `channel` і `global` перекривають її. Builtin і callee звичайного виклику причиною `call` не є. `go` випускає і аргументи, і значення функції.
- `TestSiteCount` лишається перевіркою кількості місць; текст категорії звіряє `TestCorpus`.
- Прийняття: `gofmt -l owngo/census` порожньо, `go vet ./owngo/census/` без зауважень, `golangci-lint` (нові знахідки від HEAD) без зауважень. `go test -race -count=1 ./owngo/census/` зелений: `TestCorpus` і `TestSiteCount`.
- Лишилось: P0.5, циклічні типи в двох режимах для інтерфейсів. `go fix` досі пропонує `strings.Cut` у вже наявному `quotedWants`; не чіпав.

## 2026-10-29 — P0.5

- Граф типів у `cyclic`. Спільні ребра: вказівник, елемент слайса й масиву, значення мапи, поля структури. Канал, функція і ключ мапи ребра не дають. Діагностика v0 лишається строгою: поле-інтерфейс ребра не дає, тож корпус `// want` не змінюється.
- Консервативний режим додає з інтерфейсу ребро до кожного конкретного типу всесвіту, який можна йому присвоїти. Всесвіт передає викликач; у тесті це іменовані типи пакета і вказівники на них. Тип з іншого пакета до графа не входить.
- `TestCyclicTypes`: список, дерево з батьківським вказівником, графи (`[]*T`, `map[int]*T`, `[2]*T`, взаємні `left`/`right`) циклічні в обох режимах. Через інтерфейс (`any`, власний інтерфейс, `[]edge`, два типи через спільний інтерфейс, `error`, зворотний вказівник з іншого типу пакета) — лише консервативний. Інтерфейс без зворотного шляху, канал, `func` і ключ мапи — жоден режим.
- Прийняття: `gofmt -l owngo/census` порожньо, `go vet ./owngo/census/` без зауважень, `golangci-lint` (нові знахідки від HEAD, базовий конфіг) без зауважень. `go test -race -count=1 ./owngo/census/` зелений: `TestCorpus`, `TestSiteCount`, `TestCyclicTypes`, `TestCyclicNil`.
- Лишилось: P0.6, CLI і JSON. `go fix` досі пропонує `strings.Cut` у `quotedWants`; не чіпав.

## 2026-10-29 — P0.6

- Аналізатор повертає `[]Site`: `category`, `reason`, `pos` (`file:line` без колонки) і `type` (тип значення SSA відносно пакета). Текст діагностик `// want` не змінювався. Немає місць — порожній зріз, у JSON `[]`, не `null`. Порожня `reason` лишається ключем.
- Команда `owngo/census/cmd/census -json`. Успіх: код 0, JSON лише на stdout. Без `-json` або без пакета: код 2. Помилка завантаження чи аналізу: код 1, stdout порожній. `-h`: код 0.
- Каталог `testdata` для `go list` невидимий, тож він вантажиться як GOPATH (`GO111MODULE=off`, пакети під `src`). Інший каталог, зокрема модуль поза поточним, вантажиться через `packages.Load` з `Dir` цього каталогу.
- Тест команди збирає бінарник і запускає його: `go run` замінює ненульовий код виходу на 1. Зразок `append` у `implicit.go` стоїть на рядку 6 — це рядок інструкції SSA, тип varargs лишається `*[1]int`.
- Прийняття: `gofmt -l owngo/census` порожньо, `go vet ./owngo/census/...` без зауважень, `golangci-lint` (нові знахідки від HEAD) без зауважень. `go test -race -count=1 ./owngo/census/` зелений. `go run ./owngo/census/cmd/census -json owngo/census/testdata` дає валідний JSON: 73 місця, ключі `category`, `reason`, `pos`, `type`.
- Лишилось: P0.7, розрив із escape-діагностиками `gc`. `go fix` досі пропонує `strings.Cut` у `quotedWants`; не чіпав.

## 2026-10-29 — P0.7

- `StackGaps` порівнює місця census поза `stack` з escape-діагностиками gc. Команда: `go build -gcflags=-m -json=0,file://…`. JSON (коди `escape`, `escapes`) фіксує лише втечу в купу; «does not escape» дає `-m`. Розрив — рядок з алокацією, що не тікає, без діагностики купи на тому самому рядку. Голий ідентифікатор (параметр) розриву не дає. `make(chan)` gc не діагностує і в купі лишає завжди.
- Корпус: 73 місця, 65 поза `stack`, **26** розривів. Список у `TestCorpusGCGap`.
- Сервіс [ms-faq](https://gitlab.datahub.ua.energy/tko/ms-faq) `develop` @ `520cbc6`: 701 місце, 618 поза `stack`, **45** розривів (14 у `internal/testutil`). Звіт — `owngo/GC-GAP.md`.
- Прийняття: `gofmt -l owngo/census` порожньо, `go vet ./owngo/census/` без зауважень, `golangci-lint` (нові знахідки від HEAD) без зауважень. `go test -race -count=1 ./owngo/census/` зелений.
- Лишилось: P0.8, динамічні ваги. `go fix` досі пропонує `strings.Cut` у `quotedWants`; не чіпав.
