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

## 2026-10-29 — P0.8

- `ParseAllocProfile` читає текст `go tool pprof -raw` (`alloc_objects`, `alloc_space`). `Weigh` приписує зразок першому кадру від листа до кореня, чий `file:line` збігається з місцем census. Кілька місць на одному рядку ділять об'єкти і байти цілим діленням, остача — ранішим. `FormatWeights` друкує таблицю часток; маса поза census — окремий рядок від усього профілю.
- pprof дописує повне ім'я узагальненої функції в дужках одразу після `s=рядок`. Без цього локація зникала і зразок падав з `ErrProfileLocation`.
- Сервіс [ms-faq](https://gitlab.datahub.ua.energy/tko/ms-faq) `develop` @ `520cbc6`: юніт-тести, `-memprofilerate=1`, 11 пакетів (`cmd/app` без тестів; `//go:build integration` не збирався; бенчмарків немає). 701 місце census. Серед приписаної купи `static` + `rc-local` — 76.0% об'єктів і 58.9% байтів. Поза census — 86.9% об'єктів і 98.8% байтів профілю. Звіт — `owngo/WEIGHTS.md`.
- Прийняття: `gofmt -l owngo/census` порожньо, `go vet ./owngo/census/` без зауважень, `golangci-lint` (нові знахідки від HEAD) без зауважень. `go test -race -count=1 ./owngo/census/` зелений.
- Лишилось: P0.9, пік горутин. `go fix` досі пропонує `strings.Cut` у `quotedWants`; не чіпав.

## 2026-10-29 — P0.9

- Go-агент у prod APM уже пише `golang.goroutines` (`metricset` `app`, поточна кількість). Запасний шлях `runtime/metrics` не знадобився.
- Вікно 2026-10-22T00:00:00Z … 2026-10-30T00:00:00Z, середовище `prod`. Список — усі 19 Go-сервісів, що надіслали поле. `ms-faq` серед них немає.
- Пік одного процесу: `ms-zv` 20740 (воркер; веб ≤ 65), `ms-ap` 4441 (p99 924), решта ≤ 173. Звіт — `owngo/GOROUTINES.md`. Поріг «тисячі» з `INTENT.md` перетинають два сервіси; рішення щодо M:N лишається на P0.12.
- Лишилось: P0.10, звіт фази 0. `go fix` досі пропонує `strings.Cut` у `quotedWants`; не чіпав.

## 2026-10-29 — P0.10

- Звіт `owngo/REPORT-PHASE0.md`. Census v0, розрив із gc і ваги алокацій по `GOROOT/test` Go 1.27.3 і по 18 Go-сервісах зі списку P0.9 плюс `ms-faq`. Піки горутин перенесені з P0.9, APM повторно не знімався.
- `GOROOT/test`: 1924 тести з директивою компіляції, 1667 пакетів, 13002 місця. 247 пакетів пропущено, з них 236 — кілька `package` в одному `*.dir` (harness збирає їх `go tool compile`, не `go/packages`). Ваги — 1047 програм `// run` з профілем; `static`+`rc-local` покривають 35.2% об'єктів приписаної купи і 3.6% байтів.
- 18 сервісів: 25266 місць, розривів 1413 (6.3% поза `stack`). `rc-local:call` — 15309 із 18900 місць `rc-*` (81.0%). `ms-decrypt` у клоні — Python, без census. `ms-customers` без тестів. 16 пакетів тестів упали (Postgres, таймаут 20 с, abort trap); три з них без профілю.
- Консервативний режим (`-conservative`, ребра інтерфейсу лише до іменованих типів того самого пакета) піднімає `cyclic` з 190 до 15295 на 18 сервісах і з 75 до 7029 на GOROOT. Строга діагностика лишається типовою. Каталог модуля тепер вантажиться як `./...`.
- Умова P0.11 виконана: головна причина `rc-*` — `call`. v1 не реалізовано. Поріг гейта і трек M:N не записані.
- Прийняття: `gofmt -l owngo/census` порожньо, `go vet ./owngo/census/...` і `golangci-lint run ./owngo/census/...` без зауважень, `go test -race -count=1 ./owngo/census/` зелений. `go fix` досі пропонує `strings.Cut` у `quotedWants`; не чіпав.
- Лишилось: P0.11 (зведення функцій, бо головна причина `rc-*` — аргументи викликів), потім P0.12.

## 2026-10-30 — P0.11

- Зведення параметрів v1: позичання, переміщення або спільне користування. Прапор `-v1`. Корпус v0 лишає причину `call`. Очікування v1 — `testdata/src/v1` і `v1user` (facts між пакетами). `go` як і раніше `rc-shared: go`.
- phase0 не вмикає `FactTypes` на граф імпорту: на `ms-faq` такий прохід будував SSA залежностей і не вміщався в пам'ять. Пакети модуля йдуть по одному, зведення експортованих функцій тримає сховище процесу. Виклик у stdlib і поза `-dir` лишається невідомим, тобто `shared`. Це слабше за речення плану «stdlib — тим самим аналізом».
- Повтор P0.8 і P0.10 на тих самих профілях. Ваги v0 `GOROOT/test` збіглися з таблицею P0.10 побайтово (12998 місць, `-dir /private/tmp/...`). На 18 сервісах `call` 15309/18900 (81.0%) зникає: 712 місць стають `static`, 14470 лишаються `rc-local:shared`. `move` на сервісах немає; у `GOROOT/test` — 4 місця `rc-shared:move`. Частка `static`+`rc-local` у приписаній купі не змінилась: `ms-faq` 76.0%/58.9%, 17 сервісів 14.2%/27.8%, `// run` 35.2%/3.6%. Поріг гейта і трек M:N не записані.
- Прийняття: `gofmt -l owngo/census` порожньо, `go vet ./owngo/census/...` і `golangci-lint run ./owngo/census/...` без зауважень (`0 issues`), `go test -race -count=1 ./owngo/census/` зелений. `go fix` досі пропонує `strings.Cut` у `quotedWants`; не чіпав.
- Лишилось: P0.12, поріг гейта і рішення щодо M:N в `INTENT.md`.

## 2026-10-30 — P0.12

- Поріг гейта: понад 50% байтів купи, приписаної census (усе крім `stack`), у `static` і `rc-local` разом, на сумі сервісів компанії з профілем юніт-тестів. Частка об'єктів, медіана по сервісах і `GOROOT/test // run` поріг не замінюють.
- Сума 17 сервісів — 636 572 452 з 2 286 316 727 байтів (27.8% байтів, 14.2% об'єктів). Поріг не пройдено. Медіана часток байтів по 18 сервісах із профілем — між 64.2% і 75.5% (13 вище 50%, п'ять нижче). `ms-faq` — 58.9% байтів. `// run` — 3.6% байтів. v1 частку суми не змінив. `static` після v1 — 12.1% суми. Поза census — 67.6% байтів профілю 17 сервісів.
- Рішення: переглядаємо. Фаза 1 не стартує, доки P1.0 не розпише задачі під цей замір.
- Поріг M:N: 1000 одночасних горутин в одному процесі, пік `golang.goroutines` у prod. Перетнули `ms-zv` (воркер 20740) і `ms-ap` (4441). Трек для них потрібен і фази пам'яті не випереджає. Розмір стеку лишається відкритим.
- Записано в `INTENT.md`. Прийняття — наявність порога, рішення і закриття двох відкритих питань у цьому файлі.
- Лишилось: P1.0, план фази 1 після гейта.

## 2026-10-30 — P1.0

- Фаза 1 у `PLAN.md`: P1.1 розклад байтів поза census, P1.2 `runtime.Free` під тегом `owngo` поверх `FreeRoot`, P1.3 `free` лише для `static` правилами census v1, P1.4 ASan, P1.5 лічильник звільнених байтів, P1.6 вибір наступного ковша. M:N у ці задачі не входить.
- Межі з P0.12 стоять у шапці фази: `static`+`rc-local` — 27.8% приписаної купи, `static` після v1 — 12.1% тієї суми (277 779 290 байтів), поза census — 67.6% профілю 17 сервісів. `rc-local` у фазі 1 не звільняється. Поріг «більшість купи» до цих задач не застосовується.
- Рядок фази 1 в таблиці `INTENT.md` вказує на ці задачі.
- Лишилось: P1.1, розклад байтів поза census.

## 2026-10-30 — P1.1

- `Outside` розкладає вибірки, які `Weigh` лишає поза census, на один ківш: стандартна бібліотека, модуль поза `-dir`, `*_test.go` (і `_testmain.go`), пакет `runtime`, нетестовий файл `-dir` без місця. Кадр пакета `runtime` провалюється до наступного; `runtime/pprof` лишається в стандартній бібліотеці.
- Фікстура pprof у `TestOutsideProfile` тримає ці кішші й звіряє суму з рядком `Unmatched`.
- Ті самі профілі, що в P0.8 і P0.10. На 17 сервісах поза census 4 963 968 984 байти (67.6% профілю): stdlib 48.7%, поза `-dir` 44.8%, `*_test.go` 6.0%, runtime 0.3%, у модулі 0.2%. У `ms-faq` 160 224 496 байтів (98.8%): stdlib 71.7%, поза `-dir` 27.7%. Сума ківшів збігається з рядком поза census. Звіт — `owngo/REPORT-PHASE1.md`.
- Прийняття: `gofmt -l owngo/census` порожньо, `go vet ./owngo/census/` без зауважень, `golangci-lint` без зауважень. `go test -count=1 ./owngo/census/` і `go test -race -count=1 ./owngo/census/` зелені.
- Лишилось: P1.2, `runtime.Free` під тегом `owngo`.

## 2026-10-30 — P1.2

- Тег `owngo` у `llgo` дописує `nogc`, тож збираються файли рантайму `nogc`. `runtime.Free` (`runtime/internal/runtime/free_owngo.go`) кличе `FreeRoot`: блок `malloc` звільняється, `zerobase` лишається. Інші алокації самі не звільняються.
- Програма `owngo/runtimefree`: `AllocU(32)` / `Free` (блок зникає з `malloc_size`) і `Free` від `AllocU(0)`. `LLGO_ROOT=$PWD` і LLVM 23 першим у `PATH`: `go run ./cmd/llgo run -tags owngo ./owngo/runtimefree` — код 0.
- `go test -timeout 40m ./...` без тега `owngo`: падіння лише з `owngo/BASELINE.md` (`cmd/internal/browser`, `internal/build` — архів і wasm SjLj, `internal/targets` — Memory64).
- Прийняття: `gofmt -l` на змінених файлах порожньо. `go test -count=1 -run TestApplyOwngoTag ./internal/build/` і `go test -race -count=1 -run TestApplyOwngoTag ./internal/build/` зелені. `golangci-lint` (нові знахідки від HEAD, базовий конфіг) — 0 зауважень для `./internal/build/`, `./owngo/runtimefree/` і `runtime/internal/runtime` з тегами `owngo,nogc`.
- Лишилось: P1.3, вставка `free` лише для `static`.

## 2026-10-30 — P1.3

- Прохід у `cl` за `Options.OwnGoFree` бере категорії з `census.StaticUses` (ті самі правила v1, без другого класифікатора). У набір входять функції пакета, їхні анонімні функції і вже наявні методи через `FuncValue`; нові методи прохід не створює. `runtime.Free` стоїть після останнього читання в блоці, з якого об'єкт уже не виходить живим. `stack` лишається на стеку. `rc-local`, інша категорія і невідомий виклик у stdlib чи чужий модуль `free` не отримують.
- Гілка і цикл, де читання ще попереду, `free` не ставлять: об'єкт доживає до виходу. Місце, яке тримає `defer`, теж не звільняється. У `map` і `chan` звільняється корінь, внутрішні блоки рантайму лишаються.
- `packageSummaries(nil, …)` не публікує факти і не читає зведення попереднього проходу процесу: виклик поза набором лишається невідомим. Тег `owngo` вмикає `OwnGoFree`; без прапорця IR не змінюється.
- Прийняття: `LLGO_ROOT=$PWD go test -count=1 -v ./cl/ -run OwnGoStaticFree` — `--- PASS: TestOwnGoStaticFree (0.86s)`. `gofmt -l` на змінених файлах порожньо. `go vet ./owngo/census/` і `go vet ./cl/` без зауважень. `go test -race -count=1 ./owngo/census/` зелений. `golangci-lint` (нові знахідки від HEAD, базовий конфіг) — 0 зауважень для `./cl/`, `./owngo/census/` і `./internal/build/`. `go vet ./internal/build/` як і раніше падає на копію lock у `windows_artifacts_test.go` (поза цим diff). `go fix -diff` пропонує чужі модернізації; не чіпав.
- Лишилось: P1.4, ASan на вставленому `free`.

## 2026-10-30 — P1.4

- `-asan` для `build`, `run`, `install` і `test` інструментує нативну збірку хоста. Пайплайн LLVM отримує суфікс `,asan`, кожна визначена функція — атрибут `sanitize_address`, лінк — `-fsanitize=address`. Прапорець входить у відбиток кешу (`ASAN`), тож архів без ASan не перевикористовується. Сам `-asan` не вмикає `-O0`: на `Os`/`O2` LLVM може стерти читання після `free` як невизначену поведінку, тому фікстури збираються з `-O0`. Ціль не хоста, включно з wasm, — помилка.
- `owngo/asan/uaf` після вставленого `runtime.Free` читає об'єкт. `owngo/asan/clean` проганяє види місць `static` з корпусу і лишає об'єкт `rc-local` живим до останнього читання.
- `free` не ставиться у блоці, якого алокація не домінує: інакше LLVM відхиляє модуль `time` (`Instruction does not dominate all uses`). Порожній запис (`store {}`) не випускається: ASan перевіряє ще й байт перед адресою, а `AllocU(0)` повертає `zerobase` на межі редзони `zeroVal`. Вказівник, записаний у пам'ять, не звільняється: наступне читання йде вже не з того значення SSA. Інакше `hashGrow` звільняв `new(mapextra)` перед записом `nextOverflow` (heap-use-after-free 24 байти).
- Прийняття, з кореня репозиторію, `PATH` з LLVM 23 і LLD попереду, `LLGO_ROOT=$PWD`:
  - `go run ./cmd/llgo run -tags owngo -O0 -asan ./owngo/asan/uaf` — код 1. У виводі `ERROR: AddressSanitizer: heap-use-after-free` і `SUMMARY: AddressSanitizer: heap-use-after-free ... in main.main+0x88`. Раннер загортає abort trap у `exit status 1`.
  - `go run ./cmd/llgo run -tags owngo -O0 -asan ./owngo/asan/clean` — код 0, stdout `asan-clean-ok`, тексту AddressSanitizer немає.
- `gofmt -l` на змінених файлах порожньо. `go vet` для `./ssa/`, `./cl/`, `./cmd/internal/flags/` і `./cmd/llgo/` без зауважень. `go vet ./internal/build/` як і раніше падає на копію lock у `windows_artifacts_test.go` (поза цим diff). `go test -race -count=1` зеленим пройшов `TestStoreZeroSizedEmitsNoMemoryAccess`, `TestOwnGoStaticFree`, `TestOwnGoFreeBranchAllocDominates`, `TestOwnGoFreeStoredPointerStaysLive` і сусідні перевірки `ssa`/`cl`. Прапор і пайплайн: `TestASanBuildFlag`, `TestLLVMPassPipeline`, `TestASanRequiresNativeHost`. `golangci-lint` (нові знахідки від HEAD, базовий конфіг) — 0 зауважень. `govulncheck` не запускався: залежностей не змінювали.
- Лишилось: P1.5, лічильник байтів, що пройшли через `free`.

## 2026-10-30 — P1.5

- `census.Sites` віддає категорію v1 кожного місця і читачів `static` одним проходом. Прохід `OwnGoFree` кличе `runtime.NoteAlloc(size, cat)` після купового `new` з відомим `SizeOf` і `runtime.FreeAccounted` замість `runtime.Free` на останньому читанні `static`. `stack`, нульовий розмір і місце без категорії в лічильник не входять. `MakeSlice` і решта без сталого розміру кореня як і раніше звільняються, якщо вони `static`, але байти кореня не рахуються.
- На `os.Exit` гачок `ReportExit` друкує `owngo freed` і `owngo live <категорія>`. Без тега `owngo` гачок порожній.
- Прийняття, з кореня, `PATH` з LLVM 23 і LLD попереду, `LLGO_ROOT=$PWD`:
  - `go run ./cmd/llgo run -tags owngo ./owngo/bytes/static` — код 0. `owngo freed 112`, `owngo live static 984`, `owngo live rc-local 1768`, `owngo live rc-shared 368`, `owngo live pinned 2892`. Місце `new([8]byte)` додало 8 до звільнених.
  - `go run ./cmd/llgo run -tags owngo ./owngo/bytes/rclocal` — код 0. `owngo freed 104`, `owngo live static 984`, `owngo live rc-local 1776`, `owngo live rc-shared 368`, `owngo live pinned 2892`. Ті самі 8 байтів лишились у `rc-local`.
- Числа записані в `owngo/REPORT-PHASE1.md` поруч із 12.1% (`static` після v1) і 67.6% (поза census). Частка звільненого менша за половину профілю.
- `gofmt -l` на змінених файлах порожньо. `go vet ./cl/` і `go vet ./owngo/census/` без зауважень. `go vet -tags owngo ./internal/runtime/` у модулі `runtime` не додає зауважень у нових файлах; старі `unsafe.Pointer` у `caller.go`, `local_context_stub.go`, `stubs.go`, `z_gc.go` були й раніше. `go test -race -count=1 ./owngo/census/` зелений. `go test -race -count=1 ./cl/ -run 'TestOwnGoFreeBytes|TestOwnGoStaticFree|TestOwnGoFreeBranch|TestOwnGoFreeStored'` зелений. `golangci-lint` (нові знахідки від HEAD, базовий конфіг, для рантайму ще `--build-tags owngo`) — 0 зауважень. `go fix -diff` для `./cl/` і `./owngo/census/` не чіпає цей diff. `govulncheck` не запускався: залежностей не змінювали.
- Лишилось: P1.6, гейт фази 1.

## 2026-10-30 — P1.6

- Найбільший ківш, що лишився після `static`, — байти поза census: 4 963 968 984, 67.6% профілю 17 сервісів (7 339 855 680). Сума сама перетинає половину профілю, тож поріг 50% до неї застосовується.
- Розклад P1.1: стандартна бібліотека 2 418 622 160 (48.7% рядка, 33.0% профілю), модулі поза `-dir` 2 225 338 912 (44.8%, 30.3%), `*_test.go` 299 542 056 (6.0%), runtime 12 575 544 (0.3%), у модулі 7 890 312 (0.2%). Разом два перші рядки — 63.3% профілю. У `ms-faq` поза census 160 224 496 (98.8%): стандартна бібліотека 71.7%, поза `-dir` 27.7%.
- `cyclic` 1 528 993 142 (66.9% приписаної купи). Без `ms-export` — 40 717 942 (1.8%); ця сума половину не перетинає, тож поріг `cyclic` не обирає. `rc-local` v1 — 358 793 162 (15.7%), `implicit` — 120 109 933 (5.3%). Ваги `cyclic` і `implicit` лишились з v0: v1 переніс байти лише між `static` і `rc-local`.
- P1.5: фікстура звільнила 8 байтів місця `static` (`freed` 112 проти 104). Поруч із заміром лишаються 12.1% приписаної купи і 67.6% профілю. Порядок ківшів фікстура не змінює.
- M:N без змін: трек для `ms-zv` і `ms-ap` фази пам'яті не випереджає.
- Записано в `INTENT.md`. Наступна фаза — P2.0, ще не розписана. Фази 2–4 у таблиці лишаються гіпотезою.
- Лишилось: P2.0, план наступної фази за ковшем поза census.

## 2026-10-30 — P2.0

- Фаза 2 у `PLAN.md`: P2.1 пакети рядка stdlib, P2.2 модулі поза `-dir`, P2.3 категорії v1 для stdlib, P2.4 категорії v1 для чужих модулів, P2.5 гейт. M:N у ці задачі не входить. Компілятор фаза не змінює.
- Межі з P1.6 стоять у шапці фази. Поза census — 4 963 968 984 байти, 67.6% профілю 17 сервісів. Стандартна бібліотека — 2 418 622 160 (48.7% рядка, 33.0% профілю), модулі поза `-dir` — 2 225 338 912 (44.8%, 30.3%). Разом 4 643 961 072 байти, 93.6% рядка і 63.3% профілю. У `ms-faq` ті самі два рядки — 114 923 904 (71.7%) і 44 337 552 (27.7%).
- `*_test.go` (6.0%), runtime (0.3%) і файл модуля без місця (0.2%) у задачі не входять. `cyclic` без `ms-export` — 40 717 942 байти (1.8% приписаної купи) і фазу не задає. Поріг 50% стоїть лише в гейті P2.5 і лише якщо нова сума сама його перетинає.
- Класифікатор — наявний v1. Пакети по одному, зведення через сховище процесу. `FactTypes` на весь граф імпорту не вмикається. Виклик у пакет без зведення лишається `shared`. У набір входять лише пакети, які тримають байти рядка.
- Рядок фази 2 в таблиці `INTENT.md` вказує на ці задачі. Фази 3 і 4 лишаються гіпотезою. Алокатор із пошуком бази фаза 2 не обирає.
- Лишилось: P2.1, розклад рядка stdlib за пакетами.
