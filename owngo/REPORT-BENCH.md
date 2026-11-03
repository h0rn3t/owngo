# Єдиний набір заміру (S1.1, S1.2; база гейта A)

Скрипт — `owngo/bench/compare.py PROGRAM [N] [RUNS] [--json FILE]`; `compare-binarytrees.sh`, `compare-fannkuchredux.sh`, `compare-mandelbrot.sh` — обгортки над ним (типово 10 прогонів), їх же кличе workflow `owngo-binarytrees.yml`. Варіанти кожної Go-програми: `go` (`GOAMD64=v2`), llgo без тега, llgo `-tags owngo` з типовим `-Os` і з `-O2 -lto full`. Поруч — Rust (release, `codegen-units=1`, `-C target-cpu=ivybridge`) і C. Прогони чергуються: у кожному колі по одному прогону кожного варіанта. Медіана й мін–макс — wall clock; user, sys — медіани; пікова RSS — максимум за прогони. JSON містить кожен прогін і середовище: SHA (і чи є незакомічені зміни), версії Go, LLVM, clang, rustc, модель CPU, прапорці ISA, кількість ядер, `ulimit -a`, прапорці збірки Go, Rust і C.

Пікову RSS міряє `owngo/bench/rusage.c`, а не Python: Linux переносить `ru_maxrss` процесу через `fork` і `exec`, тож дитина Python успадковувала пікову RSS інтерпретатора. Рядки RSS попередніх таблиць цього файлу (11 МБ у fannkuch-redux) — це RSS інтерпретатора: та сама програма fannkuch-redux тепер показує 2–6 МБ.

## Варіанти C (S1.2)

Програми C з Benchmarks Game без змін коду, ліцензія — `LICENSE` у кожному каталозі, як у `*rust`. Обрано найшвидшу програму C кожної задачі на сторінці, для binary-trees — найшвидшу на `malloc`/`free` і на пулах.

| Каталог | Програма | Таблиця |
| --- | --- | --- |
| `owngo/bench/binarytreesc` | binary-trees C gcc #5 (Eckehard Berns): `malloc`/`free`, потік на глибину | звичайні алокації |
| `owngo/bench/binarytreescpool` | binary-trees C gcc #2 (Jeremy Zerfas): пули APR, OpenMP | арени |
| `owngo/bench/fannkuchreduxc` | fannkuch-redux C gcc #6 (Ilya Kurdyukov, за C++ #6 — той самий родовід, що Rust #6): SSE4.1, pthreads | — |
| `owngo/bench/mandelbrotc` | mandelbrot C gcc #6 (Kevin Miller): SSE2, OpenMP; прибрано лише BOM UTF-8 | — |

Збирає `clang` з LLVM 23 з прапорцями gcc сторінок: `-O3 -fomit-frame-pointer -march=ivybridge` (ISA як у варіанта Rust) плюс `-pthread`, OpenMP, `apr-1` чи `-mno-fma -fno-finite-math-only`. Три відхилення від сторінки, без зміни коду:

- OpenMP — `libomp` з `libomp-23-dev`. З `-fopenmp=libgomp` clang мовчки ігнорує прагми: перший замір C #2 і mandelbrot C #6 ішов в один потік (4.08 і 4.50 с). На машині розробки `libomp-23-dev` розпаковано в `~/.cache/owngo-libomp` (`LIBOMP_ROOT`): установка через apt оновила б усю збірку LLVM 23. `libapr1-dev` встановлено.
- mandelbrot C #6 — `-fgnu89-inline`: `mand8` та інші — C99 `inline` без зовнішнього визначення; gcc вбудовує кожен виклик, clang ні, і компоновка падає.
- fannkuch-redux C #6 — `-t 8`: програма типово бере 4 потоки, тобто всі ядра машини Benchmarks Game.

## Результат

Ревізія `1a278541f`. Debian 13 amd64, Intel Xeon Gold 6240R @ 2.40GHz, 8 ядер, ISA `sse4_2 popcnt avx avx2 fma bmi2 avx512f`. Go 1.27.1, LLVM 23.1.5 (clang 23.1.5), rustc 1.85.1. JSON — `owngo/bench/baseline/*.json`.

```
export LIBOMP_ROOT=$HOME/.cache/owngo-libomp/root/usr/lib/llvm-23
owngo/bench/compare-binarytrees.sh 21 10 --json owngo/bench/baseline/binarytrees.json
owngo/bench/compare-fannkuchredux.sh 12 10 --json owngo/bench/baseline/fannkuchredux.json
owngo/bench/compare-mandelbrot.sh 16000 10 --json owngo/bench/baseline/mandelbrot.json
```

Вивід кожного прогону кожного варіанта — побайтно як очікуваний (mandelbrot — SHA-256), код виходу всіх трьох — 0.

### binary-trees 21

Висновку між таблицями немає: стратегія пам'яті різна.

**Звичайні алокації**

| Variant | Median, s | Min–max, s | User, s | Sys, s | Peak RSS, MB | Output |
| --- | ---: | --- | ---: | ---: | ---: | --- |
| binarytrees, go, GOAMD64=v2 | 5.39 | 5.25–5.51 | 37.93 | 0.79 | 633 | matches |
| binarytrees, llgo | 39.48 | 38.95–40.09 | 255.04 | 1.86 | 823 | matches |
| binarytrees, llgo -tags owngo (-Os) | 49.21 | 48.30–49.79 | 374.30 | 6.40 | 475 | matches |
| binarytrees, llgo -tags owngo -O2 -lto full | 48.56 | 47.74–49.14 | 368.09 | 6.45 | 474 | matches |
| C #5, malloc + pthreads | 5.48 | 5.25–5.65 | 27.12 | 0.22 | 343 | matches |

**Арени**

| Variant | Median, s | Min–max, s | User, s | Sys, s | Peak RSS, MB | Output |
| --- | ---: | --- | ---: | ---: | ---: | --- |
| binarytreesarena, go, GOAMD64=v2 | 0.41 | 0.40–0.45 | 2.81 | 0.15 | 227 | matches |
| binarytreesarena, llgo | 0.53 | 0.52–0.57 | 3.03 | 0.46 | 360 | matches |
| binarytreesarena, llgo -tags owngo (-Os) | 0.49 | 0.48–0.54 | 3.18 | 0.24 | 359 | matches |
| binarytreesarena, llgo -tags owngo -O2 -lto full | 0.45 | 0.43–0.51 | 2.79 | 0.25 | 359 | matches |
| Rust #5, bumpalo + rayon | 0.69 | 0.67–0.87 | 3.70 | 0.22 | 322 | matches |
| C #2, APR pools + OpenMP | 1.03 | 1.02–1.20 | 5.52 | 0.38 | 175 | matches |

binarytrees, llgo -tags owngo (-Os), 18: user 41.04 s (5.51 s wall) on 8 CPUs, user 3.53 s (4.00 s wall) under taskset -c 0

### fannkuch-redux 12

| Variant | Median, s | Min–max, s | User, s | Sys, s | Peak RSS, MB | Output |
| --- | ---: | --- | ---: | ---: | ---: | --- |
| fannkuchredux, go, GOAMD64=v2 | 3.21 | 3.19–3.34 | 24.78 | 0.02 | 2 | matches |
| fannkuchredux, llgo | 12.95 | 12.60–13.72 | 100.09 | 0.04 | 6 | matches |
| fannkuchredux, llgo -tags owngo (-Os) | 5.52 | 5.50–5.61 | 42.67 | 0.02 | 6 | matches |
| fannkuchredux, llgo -tags owngo -O2 -lto full | 2.48 | 2.46–2.52 | 19.05 | 0.01 | 5 | matches |
| Rust #6, rayon | 2.19 | 2.18–2.21 | 16.91 | 0.01 | 2 | matches |
| C #6, pthreads | 1.14 | 1.12–1.29 | 8.76 | 0.00 | 2 | matches |

### mandelbrot 16000

| Variant | Median, s | Min–max, s | User, s | Sys, s | Peak RSS, MB | Output |
| --- | ---: | --- | ---: | ---: | ---: | --- |
| mandelbrot, go, GOAMD64=v2 | 1.50 | 1.49–1.51 | 11.61 | 0.06 | 33 | matches |
| mandelbrot, llgo | 0.92 | 0.91–0.94 | 6.83 | 0.05 | 37 | matches |
| mandelbrot, llgo -tags owngo (-Os) | 0.90 | 0.88–0.92 | 6.70 | 0.05 | 36 | matches |
| mandelbrot, llgo -tags owngo -O2 -lto full | 0.64 | 0.62–0.65 | 4.69 | 0.04 | 36 | matches |
| Rust #4, rayon | 0.54 | 0.53–0.58 | 3.99 | 0.04 | 33 | matches |
| C #6, OpenMP | 0.61 | 0.60–0.61 | 4.68 | 0.06 | 33 | matches |

## Що видно

- Go #2 під `owngo` — 49.2 с, у 9 разів повільніше за `go` (5.39 с) і за C на `malloc` (5.48 с), і повільніше за llgo з BDWGC (39.5 с); `-O2 -lto full` не допомагає (48.6 с). На `18` під `taskset -c 0` той самий прогін бере в 11.6 раза менше CPU, ніж на 8 ядрах: час іде на взаємодію потоків — задача S2.
- fannkuch-redux: `owngo -O2 -lto full` — 1.13× від Rust #6 (2.48 проти 2.19 с), але 2.2× від C #6 (1.14 с): C #6 сам у 1.9 раза швидший за Rust #6, як і на сторінці Benchmarks Game. Типовий `-Os` — 2.5× від Rust; вибір профілю — P-NEW-01.
- mandelbrot: `owngo -O2 -lto full` — 1.19× від Rust #4 і 1.05× від C #6 (0.64 проти 0.54 і 0.61 с), `-Os` — 1.67× від Rust.
- Арени: усі Go-варіанти швидші за Rust #5 (0.41–0.53 проти 0.69 с) і за C на пулах APR (1.03 с); RSS під llgo більша, ніж у Rust (359 проти 322 МБ), C на пулах — 175 МБ.

Рішення: merge — задачі лише міряють, компілятор не змінено.

# fannkuch-redux 12

| Variant | Median, s | Min–max, s | User, s | Sys, s | Peak RSS, MB | Output |
| --- | ---: | --- | ---: | ---: | ---: | --- |
| fannkuchredux, go, GOAMD64=v2 | 3.22 | 3.19–3.27 | 24.95 | 0.03 | 2 | matches |
| fannkuchredux, llgo | 12.92 | 12.68–13.49 | 100.13 | 0.04 | 6 | matches |
| fannkuchredux, llgo -tags owngo (-Os) | 5.54 | 5.47–6.41 | 42.79 | 0.02 | 6 | matches |
| fannkuchredux, llgo -tags owngo -O2 -lto full | 2.47 | 2.47–2.54 | 19.12 | 0.01 | 5 | matches |
| Rust #6, rayon | 2.22 | 2.19–2.33 | 16.90 | 0.01 | 2 | matches |

## mandelbrot 16000

| Variant | Median, s | Min–max, s | User, s | Sys, s | Peak RSS, MB | Output |
| --- | ---: | --- | ---: | ---: | ---: | --- |
| mandelbrot, go, GOAMD64=v2 | 1.51 | 1.49–1.57 | 11.72 | 0.06 | 33 | matches |
| mandelbrot, llgo | 0.92 | 0.91–4.73 | 6.89 | 0.05 | 37 | matches |
| mandelbrot, llgo -tags owngo (-Os) | 0.91 | 0.89–0.92 | 6.78 | 0.04 | 36 | matches |
| mandelbrot, llgo -tags owngo -O2 -lto full | 0.64 | 0.64–0.65 | 4.72 | 0.04 | 36 | matches |
| Rust #4, rayon | 0.54 | 0.53–0.57 | 3.99 | 0.05 | 33 | matches |

Максимум 4.73 с у рядку llgo — один прогін (третє коло), решта дев'ять 0.91–0.94 с; медіана його не зачіпає.

## Що видно

- Go #2 під `owngo` — 48.9 с, у 9 разів повільніше за `go` і повільніше за llgo з BDWGC (40.2 с); `-O2 -lto full` не допомагає (48.6 с). На `18` під `taskset -c 0` той самий прогін бере в 11.7 раза менше CPU, ніж на 8 ядрах: час іде на взаємодію потоків — задача S2.
- fannkuch-redux: `owngo -O2 -lto full` — 1.11× від Rust #6 (2.47 проти 2.22 с), типовий `-Os` — 2.5×; вибір профілю — P-NEW-01.
- mandelbrot: `owngo -O2 -lto full` — 1.19× від Rust #4, `-Os` — 1.69×.
- Арени: усі Go-варіанти швидші за Rust #5 (0.42–0.53 проти 0.69 с), RSS більша (358 проти 322 МБ під llgo).

Рішення: merge — задача лише міряє, компілятор не змінено.

# fannkuch-redux 12

Програма — `owngo/bench/fannkuchredux`, порт на Go програми Rust #6 з Benchmarks Game (Henry Jayakusuma, за C++ #6 Andrei Simion, Revised BSD). Перестановка — `uint64` по півбайту на елемент, обертання і перевертання — зсуви й маски замість `pshufb`, поділ роботи той самий: n·(n-1) блоків на пул робітників. Порівняння — сама Rust #6 без змін, `owngo/bench/fannkuchreduxrust` (rayon 1.12.0 у `Cargo.lock`). Командний рядок той самий, що на сторінці бенчмарку: `./fannkuchredux 12`. Очікуваний вивід — `owngo/bench/fannkuchredux/testdata/expected-12.txt` (`3968050`, `Pfannkuchen(12) = 65`).

binary-trees — `owngo/REPORT-BINARYTREES.md`.

## Оточення

Debian 13 amd64, 8 ядер, 15 ГБ RAM. Go 1.27.1. LLVM 23.1.5 з apt.llvm.org, `/usr/lib/llvm-23/bin` першим у `PATH`, `LLGO_ROOT=$PWD`. rustc 1.85.1, `RUSTFLAGS="-C target-cpu=ivybridge"`, профіль release (`opt-level=3`, `codegen-units=1`), як на сторінці Rust #6. Час — wall clock, пам'ять — `ru_maxrss` дочірнього процесу.

```
owngo/bench/compare-fannkuchredux.sh 12 3
```

Скрипт збирає всі варіанти, запускає кожен тричі і звіряє вивід з очікуваним. Той самий крок іде у workflow `owngo-binarytrees.yml`, таблиця — у підсумку job.

## Результат (після фази 9)

Ревізія `390a86878`, `owngo/bench/compare-fannkuchredux.sh 12 3`. Рядок `owngo` `-Os` доміряно окремо тим самим бінарником і тим самим способом: `tail` обрізав його у виводі скрипта.

| Варіант | Прогони, с | Найкращий, с | Пікова RSS, МБ | Вивід |
| --- | --- | --- | --- | --- |
| llgo `-tags owngo` (типовий `-Os`) | 7.05 7.24 7.05 | 7.05 | 11 | побайтно як очікуваний |
| llgo `-tags owngo -O2 -lto full` | 2.66 2.61 2.59 | 2.59 | 11 | побайтно як очікуваний |
| llgo без тега (BDWGC) | 12.99 13.00 13.46 | 12.99 | 11 | побайтно як очікуваний |
| `go` (`GOAMD64=v2`) | 3.44 3.30 3.44 | 3.30 | 11 | побайтно як очікуваний |
| Rust #6 (rayon, SSE4) | 2.24 2.27 2.29 | 2.24 | 11 | побайтно як очікуваний |

**Сповільнення `owngo` `-Os` — розміщення коду, не фаза 9.** Проти P7.2 рядок повільніший на 25%, а `go`, llgo і Rust — лише на 1–3%. Вимір на одній машині того ж дня (Xeon Gold 6240R, Cascade Lake), прогони чергувались, CPU-час (user):

| Бінарник | CPU, с |
| --- | ---: |
| `390a86878`, зібраний у `/home/coder/owngo` (рядок таблиці) | 51.4–52.0 |
| `c76ac2632` (до фази 9), зібраний у `/home/coder/owngo` | 43.1–43.6 |
| `c76ac2632`, P9.2, P9.3, P9.4, P9.7 — кожен у своєму worktree | 42.8–43.6 |
| `f95bb2189` (той самий код, що `390a86878`) у worktree | 43.6–44.0 |

Коміти фази 9 по одному не сповільнюють нічого, а той самий код, зібраний в іншому каталозі, швидкий. Машинний код `block`, `advance`, `rotate`, `reverse` і `fannkuch$1` в усіх бінарниках однаковий інструкція в інструкцію. Різняться лише адреси: під `-Os` функції не вирівнюються, тож будь-яка зміна розміру коду раніше в порядку лінкування (тут — нові `IncShared`/`DecShared` у рантаймі; каталог збірки теж впливає: той самий код у worktree дає інші розміри семи функцій `time` і `internal/poll` і інші адреси, механізм не з'ясовано) зсуває все, що йде після.

Різниця — у `runtime.AssertNegativeShift`, яку гарячий шлях кличе на кожній перестановці (у `block`, двічі в `rotate`, двічі в `reverse`). Її швидкий шлях — `test $0x1,%dil; jne; ret`. У повільному бінарнику `test` починається на зміщенні 30 від 32-байтової межі, і злита пара `test`+`jne` перетинає межу. Це умова JCC erratum на Skylake-подібних ядрах: після мікрокоду, що його закриває, такий 32-байтовий блок не кешується в DSB і щоразу декодується наново. В інших чотирьох бінарниках пара лежить усередині блоку (`test` на зміщенні 18), а збіги JCC є лише на холодному `call` паніки. Інших гарячих переходів, які збігаються з умовою лише в повільному бінарнику, немає.

Що з цим робити (не зроблено): вбудовувати перевірку зсуву в місце виклику з холодним викликом паніки замість окремої функції; або збирати з вирівнюванням переходів асемблера LLVM (`-x86-branches-within-32B-boundaries`). Рядок таблиці лишено як виміряно: це те, що отримує користувач, збираючи в цьому каталозі.

## Результат (P7.2)

| Варіант | Прогони, с | Найкращий, с | Пікова RSS, МБ | Вивід |
| --- | --- | --- | --- | --- |
| llgo `-tags owngo` (типовий `-Os`) | 5.73 5.63 5.71 | 5.63 | 11 | побайтно як очікуваний |
| llgo `-tags owngo -O2 -lto full` | 2.43 2.38 2.57 | 2.38 | 11 | побайтно як очікуваний |
| llgo без тега (BDWGC) | 13.50 12.93 13.82 | 12.93 | 11 | побайтно як очікуваний |
| `go` (`GOAMD64=v2`) | 3.19 3.25 3.20 | 3.19 | 11 | побайтно як очікуваний |
| Rust #6 (rayon, SSE4) | 2.23 2.26 2.21 | 2.21 | 11 | побайтно як очікуваний |

Окремо: `llgo build -tags owngo` і `./fannkuchredux 12` — код 0, вивід побайтно як очікуваний. `llgo run -tags owngo -O0 -asan . 7` з `ASAN_OPTIONS=detect_leaks=0` — код 0, вивід як `expected-7.txt`, тексту ASan немає. Поріг часу не ставиться.

Купи програма майже не бере: RSS 11 МБ у всіх варіантах, різниця — у вартості обчислень. Усі збірки повністю завантажують 8 ядер (CPU-час ≈ 8× wall).

## Чому `owngo` повільніший за `go`

- Типовий рівень llgo — `Os` (`internal/optlevel/optlevel.go`). Він ставить `optsize` на всі функції, і LLVM не вбудовує `reverse`, `rotate`, `advance` у `block`.
- `math/bits.ReverseBytes64` (одна `bswap`) — виклик в інший модуль, вбудовується лише з LTO.
- Шість `AssertNegativeShift` на зсувах на змінну.
- `count` у купі: `&count` у `advance` дає `AllocZ` у `block`.

`-O2` без LTO — 30.6 с CPU, `-O3` — 36.1 с, `-O2 -lto full` — 17.8 с CPU (`go` — 24 с). З `-O2 -lto full` порт швидший за `go` і на 8% повільніший за Rust #6.

## Чому llgo без тега повільніший за `owngo`

Код `block` у двох збірках майже однаковий; на одному ядрі llgo без тега не повільніший (`11`: 2.99 с проти 3.33 с; з LTO 1.44 с проти 1.35 с). На 8 ядрах — false sharing: 16-байтові `count` робітників Boehm GC кладе в одну кеш-лінію, `count[layer]++` ганяє її між ядрами; `malloc` під `owngo` має арени на потік. Копія з `count [64]uint8` під llgo без тега з LTO — 2.60 с замість 7.06 с, як `owngo` (2.55 с). `perf c2c` не знімався: `kernel.perf_event_paranoid` = 4.

## Історія заміру

| Стан | `owngo` `-Os` | `owngo` `-O2 -lto full` | llgo | `go` | Rust #6 |
| --- | --- | --- | --- | --- | --- |
| `wg.Add` + `go` | 5.74 с | 2.32 с | 12.29 с | 3.13 с | 2.14 с |
| `wg.Go` (після P7.1) | 6.39 с | 2.44 с | 11.97 с | 3.19 с | 2.14 с |
| P7.2 | 5.63 с | 2.38 с | 12.93 с | 3.19 с | 2.21 с |
| `c76ac2632` (до фази 9), 2026-11-01 | 6.05 с | — | — | — | — |
| після фази 9 | 7.05 с (JCC erratum, див. вище) | 2.59 с | 12.99 с | 3.30 с | 2.24 с |
| після фази 9, зібраний у worktree | 5.87–5.96 с | — | — | — | — |

Найкращий із трьох. Розкид `owngo` `-Os` між замірами — до 13% на тому самому коді; рядок `wg.Go` у межах цього шуму.

# mandelbrot 16000

Програма — `owngo/bench/mandelbrot`, порт на Go програми Rust #4 з Benchmarks Game (Matt Watson, TeXitoi, Volodymyr M. Lisivka, Michael Cicotti, Ryohei Machida; Revised BSD). Вісім пікселів — `[8]float64` з циклами по восьми елементах замість `F64x8`, ті самі 12 кроків по 4 ітерації з раннім виходом, коли розбіглися всі вісім і попередній байт рядка нульовий, і ще одна ітерація. Рядки бере пул робітників на атомарному лічильнику, як `par_chunks_mut` у rayon. Добутки обгорнуто в `float64(…)`: специфікація Go дозволяє зливати множення з додаванням, Rust не зливає, а вивід має збігатися побайтно. Порівняння — сама Rust #4 без змін, `owngo/bench/mandelbrotrust` (той самий `Cargo.lock`, що у fannkuch-redux). Командний рядок той самий, що на сторінці бенчмарку: `./mandelbrot 16000`. Очікуваний вивід: `owngo/bench/mandelbrot/testdata/expected-200.pbm` (md5 `cc65e64b…`, як у Benchmarks Game) і SHA-256 виводу 16000 в `expected-16000.sha256`, обидва — з виводу Rust #4.

Розмір менше 8 Rust #4 не обробляє (паніка rayon `chunk_size must not be zero`); порт пише порожнє зображення `P4\n0 0\n`.

## Результат

Ревізія `2c472a343` з цими змінами, той самий Debian 13 amd64 (Xeon Gold 6240R, 8 ядер), оточення як для fannkuch-redux вище. Rust #4 з `-C target-cpu=ivybridge` векторизує по 4 `f64` в AVX.

```
owngo/bench/compare-mandelbrot.sh 16000 3
```

| Варіант | Прогони, с | Найкращий, с | Пікова RSS, МБ | Вивід |
| --- | --- | --- | --- | --- |
| llgo `-tags owngo` (типовий `-Os`) | 1.13 1.23 1.28 | 1.13 | 36 | SHA-256 як очікуваний |
| llgo `-tags owngo -O2 -lto full` | 0.71 0.69 0.73 | 0.69 | 36 | SHA-256 як очікуваний |
| llgo без тега (BDWGC) | 0.97 1.07 1.06 | 0.97 | 37 | SHA-256 як очікуваний |
| `go` (`GOAMD64=v2`) | 1.57 1.63 1.69 | 1.57 | 33 | SHA-256 як очікуваний |
| Rust #4 (rayon, AVX) | 0.57 0.57 0.61 | 0.57 | 32 | SHA-256 як очікуваний |

Пам'ять — сам буфер зображення (16000²/8 = 32 МБ) в усіх. Причини розриву не розбиралися; `go` не векторизує цикли по восьми елементах, LLVM — може. Окремо: `llgo run -tags owngo -O0 -asan . 200` з `ASAN_OPTIONS=detect_leaks=0` — код 0, вивід побайтно як `expected-200.pbm`, тексту ASan немає. Поріг часу не ставиться.

Перша версія порту будувала вивід через `append(out, make([]byte, n)...)`: `go` розширює зріз без тимчасового буфера, llgo виділяв 32 МБ тимчасово, і RSS llgo була 66 МБ проти 33 у `go`. Порт робить одну алокацію; сама різниця в компіляторі лишається.

# binary-trees 21 (S1)

Програми — `owngo/bench/binarytrees` (Go #2 з Benchmarks Game, без змін коду), `owngo/bench/binarytreesarena` (та сама задача на аренах) і `owngo/bench/binarytreesrust` (Rust #5, bumpalo + rayon).

## Результат

Ревізія `01de22c90` (компілятор), скрипт `compare-binarytrees.sh` з S1. Debian 13 amd64, Intel Xeon Gold 6240R @ 2.40GHz, 8 ядер. Go 1.27.1, LLVM 23.1.5, rustc 1.85.1. Go #2 під `go` — `GOAMD64=v2`; llgo — типовий `-Os`, тег `owngo`; Rust — `--release` з `-C target-cpu=ivybridge`, як на сторінці Rust #5.

```
owngo/bench/compare-binarytrees.sh 21 3
```

| Варіант | Прогони, с | Медіана, с | Найкращий, с | User найкращого, с | Пікова RSS, МБ | Вивід |
| --- | --- | --- | --- | --- | --- | --- |
| Go #2, `go` | 5.50 5.46 5.51 | 5.50 | 5.46 | 38.66 | 605 | як `expected-21.txt` |
| арени, llgo `-tags owngo` | 0.50 0.50 0.49 | 0.50 | 0.49 | 3.19 | 358 | як `expected-21.txt` |
| Rust #5 | 0.69 0.69 0.71 | 0.69 | 0.69 | 3.70 | 321 | як `expected-21.txt` |

CPU-час Go #2 під `owngo` на `18`, по одному прогону: на 8 ядрах — user 40.65 с (5.42 с wall); під `taskset -c 0` — user 3.51 с (3.95 с wall).

Рядок Go #2 під `owngo` з таблиці прибрано (рішення розробника, 2026-11-03); скрипт будує цей варіант лише для рядка CPU-часу на `18`. На одному ядрі той самий прогін `18` витрачає в 11.6 раза менше CPU, ніж на восьми, тобто час іде на взаємодію потоків, а не на обчислення. Це узгоджується із заміром у `INTENT.md` («Трек швидкодії»): без двох атомарних лічильників обліку Go #2 під `owngo` — 7.2 с. Задача S2 (облік без спільної кеш-лінії) спрямована саме на це. Арени під `owngo` швидші за Rust #5 (0.50 проти 0.69 с), але це інша стратегія пам'яті, і висновку між рядками звичайних алокацій та арен звіт не робить (S1.1).

Рішення: merge — задача S1 лише міряє і нічого не змінює в компіляторі.
