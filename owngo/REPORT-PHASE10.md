# Звіт фази 10

Ківш фази — `pinned` (рішення гейта фази 9, `INTENT.md`). База — корпус `owngo/bench`.

## P10.1 Розклад live `pinned` корпусу

**Метод.** Ревізія `532249fa7`, Debian 13 amd64, `/usr/lib/llvm-23/bin` першим у `PATH`, `llgo run -a -tags owngo`. Тимчасова гістограма, у репозиторій не потрапила: для місця `pinned` компілятор друкує номер місця, функцію і позицію і передає номер у старших бітах категорії `NoteAlloc`; рантайм сумує байти за номером і друкує їх на виході. `pinned` не звільняється, тож сума `NoteAlloc` за місцем — це його байти на виході. Вивід програм і звіт `owngo` — побайтно як у таблиці P9.8 (binarytrees `14` — як у P8.8: `pinned` 5 992).

Збірка корпусу має 100 місць `pinned`, усі в stdlib і рантаймі llgo; у пакетах програм місць `pinned` немає. Байти на виході дають чотири.

**Клас** — `runtime` для `runtime/internal/runtime` llgo, `stdlib` для пакетів Go, зокрема з латками llgo (`runtime/_patch`). **Глобальна** — чи досяжний блок на виході з глобальної змінної чи сховища рантайму; у `gc` такі байти теж живі.

| Функція | Рядок | Клас | Перетворення | Глобальна | binarytrees `10` | binarytrees `14` | binarytreesarena `10` | fannkuchredux `7` |
| --- | --- | --- | --- | --- | ---: | ---: | ---: | ---: |
| `mapassign_faststr`, параметр `s` | `runtime/internal/runtime/map_faststr.go:233` | runtime | `stringStructOf(&s)` і `noescape(unsafe.Pointer(&s))`, у `noescape` — `uintptr` | ні | 4 960 (310 × 16) | 4 960 | 4 960 | 4 960 |
| `(*sync.Pool).pin`, `l = &poolLocal{}` | `runtime/_patch/sync/pool.go:133` | stdlib | `unsafe.Pointer(l)` у `runtime_poolLocalSet` | так: мапа потоку рантайму | 640 (5 × 128) | 896 (7 × 128) | 0 | 0 |
| `(*sync.Pool).pinSlow`, `l := &poolLocal{}` | `runtime/_patch/sync/pool.go:146` | stdlib | `unsafe.Pointer(l)` у `runtime_poolLocalSet` | так: мапа потоку рантайму | 128 | 128 | 128 | 128 |
| `float64frombits`, параметр `b` | `$GOROOT/src/runtime/float.go:58` | runtime | `*(*float64)(unsafe.Pointer(&b))` | ні | 8 | 8 | 8 | 8 |
| **Разом** | | | | | **5 736** | **5 992** | **5 096** | **5 096** |

Сума кожного стовпця дорівнює `owngo live pinned` того самого прогону і таблиці P9.8 (P8.8 для binarytrees `14`).

**Приріст binarytrees між `10` і `14`** — 256 байтів, два `poolLocal` по 128: `fmt` бере `pp` з `sync.Pool`, локальний кеш пулу — один на потік, а на `14` горутини займають сім потоків замість п'яти. `binarytreesarena` і `fannkuchredux` `fmt` з горутин не викликають.

**Два види.**

- *Перетворення лише в межах функції* — `mapassign_faststr` і `float64frombits`, 4 968 байтів у кожній програмі (97.5% live `pinned` binarytreesarena і fannkuchredux, 86.6% binarytrees `10`). Адреса параметра стає `unsafe.Pointer` лише щоб прочитати ті самі байти іншим типом; вказівник функцію не покидає. go/ssa кладе параметр в купу, бо береться його адреса, census бачить `unsafe.Pointer` і дає `pinned`. Байти ростуть з кількістю викликів: 16 на кожне `m[k] = v` з рядковим ключем. У `gc` обидва параметри живуть на стеку.
- *Адреса віддається сховищу рантайму* — `poolLocal` пулу, 768 / 1 024 / 128 / 128 байтів. Кеш живе в мапі потоку до виходу з потоку (коментар у `runtime/_patch/sync/pool.go`), тобто закріплення тут — правильна поведінка. У `gc` локальні кеші досяжні з `allPools` до наступного GC.

Вивід для P10.2: у корпусі місць `pinned` у пакетах головного модуля немає, тож збірка корпусу не має друкувати попереджень. Вивід для P10.5: байти, досяжні з глобальної змінної чи сховища рантайму, — 768 / 128 / 128 з 15 928 live `pinned` корпусу (binarytrees `10`, binarytreesarena, fannkuchredux).
