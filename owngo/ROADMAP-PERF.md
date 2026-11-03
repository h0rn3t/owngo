# OwnGo — Performance Roadmap до рівня Rust / C

**Базова архітектура:** Go 1.27 → llgo / LLVM 23 → ownership analysis → `owngo` runtime без трасувального GC
**Дата укладання:** 2026-11-01
**Статус:** прийнято як розширення треку швидкодії (рішення розробника 2026-11-01). Джерело правди лишається [`INTENT.md`](../INTENT.md) (цілі й рішення) і [`PLAN.md`](../PLAN.md) (задачі з критеріями прийняття). Цей файл — обґрунтування і повна карта етапів; задачі з'являються в `PLAN.md` після гейта попереднього етапу.

## 1. Мета та принципи

**Мета:** компілювати звичайний, незмінений Go-код у нативний бінарник із продуктивністю, близькою до Rust/C, без трасувального GC і без порушення семантики Go.

Пріоритети:

1. **Коректність вища за швидкість.** Жодна оптимізація не має вводити use-after-free, double-free, data race або нові неконтрольовані витоки.
2. **Порівнюємо однакову роботу.** Однакові алгоритми, результати, конкуренція, політика алокації (арени порівнювати з аренами), апаратне забезпечення й умови запуску.
3. **Спочатку вимірювання.** Кожна зміна має baseline, профіль, експеримент, критерій приймання та звіт до/після.
4. **Автоматично, без нових вимог до Go-коду.** Регіони, borrow і RC elision виводяться компілятором; за браку доказу — консервативний fallback.
5. **Production і debug — окремі профілі.** Важку діагностику можна відключати у production лише за наявності тестованого діагностичного режиму.
6. **Не плутати близькість до Rust із перемогою в одному benchmark.** Загальний успіх визначається за корпусом програм і показниками серверів.

### Цільові показники (гіпотези, а не обіцянки)

| Напрям | Ціль | Пояснення |
|---|---|---|
| CPU-bound ядра без алокацій | медіанний час OwnGo ≤ 1.2× Rust/C | Лише на порівнянному коді, CPU й ISA |
| Memory-intensive ядра | медіанний час ≤ 1.2–1.5× Rust/C | На підмножині з еквівалентною стратегією пам'яті |
| Peak RSS | ≤ 1.2× Rust у цільових сценаріях | Не універсальна вимога для всіх програм |
| Порівняння зі стандартним Go | throughput не гірше 1.2× за часом; p99/RSS кращі в обраних сценаріях | Гейт паритету з `INTENT.md` |
| Коректність | 0 нових регресій відносно baseline llgo | Differential tests, sanitizers, fuzzing, long-running tests |
| Незмінений Go-код | жодних обов'язкових анотацій | Включно з рекурсивними функціями-будівниками |

## 2. Поточна вихідна точка

Показники взято зі звітів репозиторію; **це не нові незалежні вимірювання**.

| Тест | Standard Go | OwnGo | Rust | Примітка |
|---|---:|---:|---:|---|
| `binary-trees 21`, звичайний Go #2 | 5.93 с | 48.65 с | 0.67 с | Rust #5 використовує арену; порівняння політик алокації нееквівалентне |
| `binary-trees 21`, Go з ручною ареною | 0.47 с | 0.43 с | 0.67 с | Результат із README; не доводить автоматичного виведення регіонів |
| `fannkuch-redux 12`, OwnGo `-O2 -lto full` | 3.30 с | 2.59 с | 2.24 с | Результати після фази 9 |
| `fannkuch-redux 12`, OwnGo типовий `-Os` | 3.30 с | 7.05 с | 2.24 с | Виявлена чутливість до розміщення коду/JCC erratum |

Джерела: [`README.md`](../README.md), [`REPORT-BENCH.md`](REPORT-BENCH.md), [`REPORT-BINARYTREES.md`](REPORT-BINARYTREES.md), [`GC-GAP.md`](GC-GAP.md), [`GOROUTINES.md`](GOROUTINES.md).

**Встановлені вузькі місця / гіпотези:**
- Атомарні глобальні лічильники `NoteAlloc`, `FreeAccounted`, `DecAccounted`, `cyclicFreed` і memory profiler можуть спричиняти cache-line contention.
- Типовий `-Os` обмежує inlining та інші оптимізації; у `fannkuch-redux` важливі LTO, shift checks і розміщення інструкцій.
- У `binary-trees` велика ціна індивідуальних виділень, RC та `DecCyclic` замість регіонального звільнення.
- Власний size-class allocator вже є, тому змінювати його до профілювання не варто.
- Поточна модель горутин llgo: 1 горутина = 1 OS thread. Для серверних піків у тисячі горутин потрібен окремий трек M:N.
- Фази 0–10 закриті (гейт P10.6); відкриті P11.0 (витік `pinned` у рантаймі llgo) і P11.1 (план паритету).

**Обмеження машини розробки (Debian 13, sandbox):** `perf` недоступний (`perf_event_paranoid=4`), TSan не запускається (ASLR не вимикається), `/usr/bin/time` немає — RSS і CPU-час міряються через `getrusage`. Гейти, що вимагають hardware counters або TSan, потребують іншого стенда (CI або виділений runner); без нього вимога відмічається в звіті як «не перевірено».

## 3. Roadmap: пріоритети та залежності

| Черга | Потік | Результат | Залежить від |
|---|---|---|---|
| 0 | Baseline / CI | Надійний контроль продуктивності | — |
| 1 | Accounting / codegen quick wins | Відсутність необґрунтованих накладних витрат | Baseline |
| 2 | Ownership + RC elision | Менше heap allocations і `Inc/Dec` | Baseline, verifier |
| 3 | Compiler-inferred regions | `FreeRegion` замість каскадного звільнення | Ownership summaries |
| 4 | LLVM codegen parity | Оптимізований код CPU-bound задач | Baseline |
| 5 | Allocator optimization | Підвищення throughput та зменшення RSS за профілем | Regions + measurements |
| 6 | M:N scheduler | Масштабування конкурентних сервісів | Коректна ownership/runtime модель |
| Паралельно | Correctness / Phase 11 | Безпека та сумісність | Кожна зміна |

> Нумерація **S1–S4** відповідає наявному треку швидкодії в `PLAN.md`. Назви **P-NEW-*** — нові задачі треку.

## 4. Етап A — Baseline і performance regression suite (P0)

### S1 — завершити наявне порівняння

- [x] Уніфікувати `owngo/bench/compare-binarytrees.sh` і `compare-fannkuchredux.sh`.
- [x] Додати C (Clang) і Rust (rustc) варіанти з однаковими алгоритмами.
- [x] Розділити `binary-trees` на **звичайні алокації** та **арени**: не змішувати їх в одному висновку.
- [ ] Зберігати SHA компіляторів, версії LLVM/Go/Rust/C, CPU model, ISA flags, ядра, `ulimit`, частоти CPU та режим LTO.
- [x] Запускати достатню кількість повторів (спочатку 10) із чергуванням варіантів; звітувати медіану, розкид і peak RSS, не лише найкращий результат.
- [ ] Додати CPU time, hardware counters (`perf stat` де доступно), allocs/sec, `inc/dec`, `DecCyclic`, bytes freed, live bytes.
- [x] Тримати порівняльний набір: `go`, `llgo`, `owngo -Os`, `owngo -O2 -lto full`, Rust, C.
- [ ] CI публікує `performance.json` та Markdown-звіт; окремий scheduled runner — для стабільних вимірів (звичайний shared CI може бути шумним).

**Гейт A:** на одному SHA автоматично відтворюються коректні результати всіх програм і порівняльна таблиця з медіаною, варіацією та RSS. Кожен наступний етап використовує цей baseline.

## 5. Етап B — зняти очевидні runtime/codegen overheads (P0)

### S2 — sharded accounting

- [ ] Замінити глобальну cache-line contention у `NoteAlloc`, `FreeAccounted`, `DecAccounted`, `cyclicFreed` на рознесені шарди (спочатку per-OS-thread; з M:N — per-logical-P).
- [ ] Уточнити правила атомарності, ініціалізації, завершення потоку, агрегації та reset.
- [ ] `Freed`, `Live`, `ReportExit` повинні отримувати ті самі точні підсумки, що і до змін.
- [ ] Додати навантаження 1/2/4/8/16 потоків, де це підтримує стенд.

### S3 — memory profiler accounting

- [ ] Аналогічно усунути contention у `memProfileAddObject` без порушення точності `MemProfile`.
- [ ] Якщо зміна стосується upstream llgo — підготувати окремий PR upstream.
- [ ] Підтвердити коректність профілів на тестах memory profiling.

### P-NEW-01 — production optimization profile

- [ ] Додати явний стабільний профіль optimized build, наприклад `-O2 -lto full`; не змінювати default без A/B вимірів.
- [ ] Перевірити `-O2`/`-O3`, ThinLTO/full LTO, PGO (лише за наявності коректного профілю) і вплив на binary size/build time.
- [ ] Впровадити усунення/інлайнінг shift checks лише за доведених умов; не порушувати Go-поведінку для від'ємного зсуву.
- [ ] Протестувати function alignment / JCC boundary mitigation на релевантних x86 CPU, зокрема сценарій із `fannkuch-redux`.

**Гейт B:** `fannkuch-redux` з продуктивним профілем у межах **1.10–1.15×** Rust на фіксованому стенді (stretch target); зменшення contention доведено hardware counters/CPU time; жодних регресій correctness.

## 6. Етап C — ownership analysis і RC elision (P0–P1)

### P-NEW-02 — міжпроцедурні ownership summaries

- [ ] Поширити аналіз `borrow / consume(move) / shared / unknown` через межі функцій і пакетів (зведення P6.5–P6.7 — свіжий результат, власник у викликача, виклик лише читає — вже є; етап їх узагальнює).
- [ ] Врахувати виклики через інтерфейси, closures, методи, рекурсію, `defer`, `panic`, `go`, канали.
- [ ] Зробити flow-sensitive аналіз aliasing, включно з `&s.f`, slices та pointer-containing maps.
- [ ] Для `unknown` залишити консервативний варіант; ніяких передчасних `free`.

### P-NEW-03 — RC elision

- [ ] Прибрати парні `Inc/Dec`, коли вони нейтралізуються на доведеному шляху.
- [ ] Виводити borrowed reads і moves, де немає другого власника (кандидат «виведення курсорів», Nim ARC).
- [ ] Оптимізувати останнє використання та передачу власності без додаткового RC.
- [ ] Реалізувати verify pass для ownership IR перед генерацією коду (кандидат «верифікатор володіння»).

### P-NEW-04 — stack allocation parity

- [ ] Систематизувати розрив із escape analysis стандартного `gc` за `owngo/GC-GAP.md`.
- [ ] Пріоритезувати часто виконувані heap allocations, які `gc` кладе на стек.
- [ ] Уточнити escape analysis для non-escaping closures, temporary arrays, локальних структур, меж функцій.
- [ ] Для кожного перетворення перевіряти stack lifetime, indirect aliases і non-local control flow.

**Гейт C:** unit-тести для `borrow/move/alias` проходять; на вибраному корпусі зменшені `Inc/Dec` і heap allocations; 0 нових ASan/TSan/диференційних збоїв. Покращення вимірюється *за кількістю операцій і часом*, а не лише за часткою статично класифікованих місць.

## 7. Етап D — compiler-inferred regions (P1, ключовий етап)

### S4 — план регіонів

- [ ] Знайти функції-будівники об'єктних графів з одним коренем (`NewTree` тощо).
- [ ] Виводити життєвий цикл графа з урахуванням рекурсії, aliases, closures, каналів і горутин.
- [ ] Позначати лише *доведено* регіонально-безпечні алокації.
- [ ] Описати критерії fallback до звичайного `static`, RC або `cyclic`.

### P-NEW-05 — region-aware codegen

- [ ] Додати `RegionCreate`, `RegionAlloc`, `FreeRegion` у внутрішній IR/runtime (конкретні API — після прототипу).
- [ ] Забезпечити propagation region context через call graph без зміни Go API.
- [ ] Вставляти `FreeRegion` у доведеній точці смерті кореня; коректно обробити return/defer/panic.
- [ ] Заборонити use-after-region-free; escape кореня чи дочірнього вузла має скасувати небезпечну оптимізацію.
- [ ] Перевірити взаємодію з `unsafe`, внутрішніми вказівниками, weak refs і cleanup/finalizer semantics.

### P-NEW-06 — region benchmark gate

- [ ] Незмінений `owngo/bench/binarytrees` автоматично використовує регіони там, де це безпечно.
- [ ] Зібрати профілі викликів `RegionAlloc`, `FreeRegion`, `DecCyclic`, allocator calls, RSS.
- [ ] Виміряти продуктивність звичайного Go #2 під OwnGo та ручної арени; не порівнювати як еквівалентні реалізації, поки стратегії не збігаються.

**Гейт D:** compiler-inferred region на `binary-trees` дає порядок продуктивності, близький до ручної арени, без модифікації вихідного Go-коду; чітка документація для випадків, де автоматичне виведення неможливе. Ціль треку з `INTENT.md`: `binary-trees 21` ≤ **1.5× Rust #5**; досягнення не гарантується.

## 8. Етап E — LLVM codegen parity (P1–P2)

### P-NEW-07 — machine-code quality

- [ ] На CPU-bound корпусі звести різницю IR та ASM між OwnGo, Clang і rustc.
- [ ] Дослідити bounds-check elimination для доведених меж.
- [ ] Додати devirtualization для викликів, де множина цілей точно відома.
- [ ] Передавати LLVM aliasing/access metadata (`noalias`, `readonly` тощо) тільки за доведених гарантій; **не** вважати звичайні Go pointers еквівалентними `&mut` Rust.
- [ ] Поліпшити SROA, inlining, loop optimizations і autovectorization.
- [ ] Перевірити вплив на code size, compile time, CPU architecture portability.

**Гейт E:** медіанний CPU time на погодженому CPU-bound корпусі — у межах **1.2× Rust/C**, без зростання числа недіагностованих помилок і без істотних compile-time регресій.

### P-NEW-10 — розмір бінарника

- [ ] Виміряти stripped-розмір кожної програми корпусу S1.1 проти Rust (`--release`, `lto = "fat"`, `codegen-units = 1`; `panic = "abort"` окремим рядком).
- [ ] Розбити розмір за секціями й пакетами: рантайм, метадані типів, `reflect`, таблиці методів.
- [ ] Видаляти недосяжний код і метадані, `--gc-sections`, ICF, обрізати таблиці методів — за розбивкою, спершу найбільші частки.

**Кінцева ціль треку (рішення 2026-11-02, `INTENT.md`):** час виконання і stripped-розмір бінарника — кращі за Rust (< 1.0×). Гейти 1.2–1.5× — проміжні.

## 9. Етап F — оптимізація алокатора за профілем (P1–P2)

### P-NEW-08 — allocation/reuse

- [ ] Виміряти `alloc/free` частоту за size class, remote frees, locks, fragmentation, RSS після звільнення.
- [ ] Перевірити lifetime-driven reuse / allocation sinking, а також FBIP для доведених безпечних перетворень.
- [ ] Лише за наявності bottleneck порівнювати власний size-class allocator із mimalloc на однакових workloads.
- [ ] Зберегти коректне відновлення бази для interior pointers і thread-safety.

**Гейт F:** статистично значуще поліпшення `allocs/sec` / RSS / throughput на проблемних workload без регресії на інших. За відсутності allocator bottleneck — етап закрити без додаткових змін.

## 10. Етап G — M:N scheduler для серверів (P2)

### P-NEW-09 — scheduler design and implementation

- [ ] Спроєктувати `M:N`: run queues, work stealing, parking/wakeup, scheduler fairness.
- [ ] Безпечно інтегрувати blocking I/O, syscalls, TLS, signal handling, C ABI та foreign calls.
- [ ] Підтвердити ownership/RC-семантику при міграції горутини між OS threads.
- [ ] Прив'язати allocator/accounting shards до логічних процесорів (аналог P), де це потрібно.
- [ ] Протестувати десятки тисяч горутин, піки навантаження, таймери, канали та cancellation.

**Гейт G:** сценарії з ≥ 20 000 горутин не впираються в OS-thread/PID limit; підтверджений throughput і p99 на HTTP/worker workload проти стандартного Go. Саме для production-сервісів із великою конкуренцією цей етап є блокуючим.

## 11. Паралельний обов'язковий трек — коректність і паритет

- [ ] Закрити P11.0 (витік `pinned` у рантаймі llgo) і P11.1 (задачі паритету).
- [ ] Документувати й перевірити поведінку finalizers, `runtime.AddCleanup`, weak references, `defer`, `panic/recover`.
- [ ] Проганяти differential tests проти Go `gc`, `GOROOT/test` і підтримуваний corpus стандартної бібліотеки.
- [ ] Використовувати ASan для UAF/double-free, TSan для data races, а для витоків — окремі leak checks (не вважати `detect_leaks=0` перевіркою витоків).
- [ ] Додати fuzzing: cycles, interior pointers, goroutines, sharing, closures, panics, channels.
- [ ] Тестувати довгоживучі програми: memory growth, periodic steady-state RSS, race/pressure cases.
- [ ] Перш ніж оцінювати production parity, зібрати хоча б один реальний сервіс без змін коду та порівняти його з `gc` за throughput, CPU, p99, RSS.

**Гейт коректності:** немає нових регресій відносно baseline llgo; усі небезпечні або неохоплені випадки мають документований fallback чи явну заборону, а не мовчазну генерацію некоректного коду.

## 12. Структура CI та звітності

Бажана структура (каталогів поки немає; з'являються разом із задачами, яким вони потрібні):

```text
owngo/
  bench/
    suites/{cpu,memory,concurrency,server}/
    results/schema.json
  reports/
    performance-baseline.md
    ownership-elision.md
    regions.md
    codegen.md
    scheduler.md
```

Кожен performance report повинен містити:

- commit SHA і toolchain versions;
- ОС, CPU, RAM, ліміти, CPU affinity/ISA flags;
- однаковість алгоритму, результуючого виводу й стратегії виділення пам'яті;
- режим збірки та LTO;
- медіану, dispersion, user/sys time, peak RSS;
- профіль bottleneck до оптимізації;
- результат після зміни й регресії в інших benchmark;
- correctness gate, який пройшла зміна;
- рішення **merge / rework / rollback**.

### Мінімальні build-сценарії

```bash
# Звичайний Go
GOAMD64=v2 go build -o /tmp/bt-go ./owngo/bench/binarytrees

# OwnGo, базовий режим
llgo build -tags owngo -o /tmp/bt-owngo ./owngo/bench/binarytrees

# OwnGo, продуктивний режим
llgo build -tags owngo -O2 -lto full -o /tmp/bt-owngo-opt ./owngo/bench/binarytrees

# Існуючі сценарії порівняння
owngo/bench/compare-binarytrees.sh 21 3
owngo/bench/compare-fannkuchredux.sh 12 3
```

Для стабільних performance gates збільшити повтори понад 3, запускати на виділеному runner і не сприймати різницю в межах шуму як покращення.

## 13. Критичний шлях

1. **S1:** повторювана baseline suite з коректним порівнянням C/Rust/Go/OwnGo.
2. **S2 → S3:** ліквідувати contention глобальних лічильників і memory profiler.
3. **P-NEW-01:** оптимізований production profile, LTO, shift-check/code layout.
4. **P-NEW-02 → P-NEW-04:** ownership summaries, RC elision, stack allocation parity.
5. **S4 → P-NEW-06:** автоматичні регіони — головна ставка для `binary-trees`.
6. **P-NEW-07:** LLVM parity після усунення зайвих операцій керування пам'яттю.
7. **P-NEW-08:** алокатор лише за підтвердженим профілем bottleneck.
8. **P-NEW-09:** M:N для high-concurrency production, після доведення коректності основної memory model.
9. **Увесь час:** фаза 11, sanitizers, differential tests, реальний production parity.

## 14. Definition of Done

- [ ] Є повторюваний benchmark corpus із Go, OwnGo, Rust, C та порівнюваними алгоритмами/алокаціями.
- [ ] У CPU-bound задачах досягнуто цільового parity або документовано причини відставання.
- [ ] На memory-heavy задачах автоматично виведені ownership/regions дають відчутний приріст без змін Go-коду.
- [ ] На реальному сервісі доведено відсутність істотної регресії throughput та, де це застосовно, поліпшення p99/RSS проти `gc`.
- [ ] Немає нових correctness regressions, UAF, double free, data races та недіагностованих витоків.
- [ ] `unsafe`, `reflect`, finalizers/cleanup і конкурентність мають перевірену семантику або задокументовані обмеження.
- [ ] Немає benchmark-specific hardcoded оптимізацій, які не узагальнюються на інший Go-код.

**Головний висновок:** послідовність — **вимірювання → sharded accounting → RC elision → автоматичні регіони → оптимізація LLVM → M:N для серверів**, із безперервним контролем коректності.
