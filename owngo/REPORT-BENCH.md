# fannkuch-redux 12

Програма — `owngo/bench/fannkuchredux`, порт на Go програми Rust #6 з Benchmarks Game (Henry Jayakusuma, за C++ #6 Andrei Simion, Revised BSD). Перестановка — `uint64` по півбайту на елемент, обертання і перевертання — зсуви й маски замість `pshufb`, поділ роботи той самий: n·(n-1) блоків на пул робітників. Порівняння — сама Rust #6 без змін, `owngo/bench/fannkuchreduxrust` (rayon 1.12.0 у `Cargo.lock`). Командний рядок той самий, що на сторінці бенчмарку: `./fannkuchredux 12`. Очікуваний вивід — `owngo/bench/fannkuchredux/testdata/expected-12.txt` (`3968050`, `Pfannkuchen(12) = 65`).

binary-trees — `owngo/REPORT-BINARYTREES.md`.

## Оточення

Debian 13 amd64, 8 ядер, 15 ГБ RAM. Go 1.27.1. LLVM 23.1.5 з apt.llvm.org, `/usr/lib/llvm-23/bin` першим у `PATH`, `LLGO_ROOT=$PWD`. rustc 1.85.1, `RUSTFLAGS="-C target-cpu=ivybridge"`, профіль release (`opt-level=3`, `codegen-units=1`), як на сторінці Rust #6. Час — wall clock, пам'ять — `ru_maxrss` дочірнього процесу.

```
owngo/bench/compare-fannkuchredux.sh 12 3
```

Скрипт збирає всі варіанти, запускає кожен тричі і звіряє вивід з очікуваним. Той самий крок іде у workflow `owngo-binarytrees.yml`, таблиця — у підсумку job.

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

Найкращий із трьох. Розкид `owngo` `-Os` між замірами — до 13% на тому самому коді; рядок `wg.Go` у межах цього шуму.
