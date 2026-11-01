# Звіт фази 9

Ківш фази — `rc-shared` (рішення гейта фази 8, `INTENT.md`). База — корпус `owngo/bench`.

## P9.1 Розклад live спільної втечі корпусу

**Метод.** Ревізія `c76ac2632`, Debian 13 amd64, `/usr/lib/llvm-23/bin` першим у `PATH`, `llgo run -a -tags owngo`. Тимчасова гістограма, у репозиторій не потрапила: для місця `rc-shared` чи `cyclic` компілятор після `NoteAlloc` кличе `NoteSite(ptr, size, id)`, де `id` — хеш функції, позиції і позначки v1. Рантайм тримає таблицю «блок → місце», `FreeRoot` прибирає з неї і адресу блока, і адресу за словом лічильника. На виході — сума байтів блоків, що лишились, за місцями. Вивід програм і звіт `owngo` — побайтно як у таблиці P8.8.

Сума рядків кожної програми дорівнює `live rc-shared` + `live cyclic` P8.8. Увесь `live cyclic` корпусу — спільна втеча: через `go` чи глобальну змінну.

**Шлях** — як вказівник виходить з функції місця: `go` — захоплення замиканням чи аргумент `go`; `move → go` — захоплення замиканням, яке передається в `(*sync.WaitGroup).Go`, а той запускає його через `go`; `global` — запис у глобальну змінну. Каналу в корпусі немає.

**Глобальна** — чи досяжний блок на виході з глобальної змінної. Такі байти і в `gc` живі до виходу.

### binarytrees `10` і `14`

| Функція | Рядок | v1 | Шлях | Глобальна | Байти `10` | Байти `14` |
| --- | --- | --- | --- | --- | ---: | ---: |
| `NewTree`, вузли `longLivedTree` | `main.go:66` | `cyclic: return` | `go` через `longLivedTree` | ні | 16 368 | 262 128 |
| `NewTree`, листя `longLivedTree` | `main.go:68` | `cyclic: return` | `go` через `longLivedTree` | ні | 16 384 | 262 144 |
| `Run`, `var longLivedTree *Tree` | `main.go:102` | `cyclic: go` | `go` | ні | 8 | 8 |
| `Run`, `outBuff` (комірка змінної) | `main.go:85` | `rc-shared: go` | `go` | ні | 24 | 24 |
| `Run`, `var wg sync.WaitGroup` | `main.go:74` | `rc-shared: go` | `go` | ні | 16 | 16 |
| `Run`, параметр `maxDepth` | `main.go:72` | `rc-shared: go` | `go` | ні | 8 | 8 |
| `ffi.init`, `typeEmpty = &Type{…}` | `runtime/internal/ffi/type.go:77` | `cyclic: global` | `global` | так | 24 | 24 |
| `ffi.init`, `Typ = []*Type{…}` | `runtime/internal/ffi/type.go:80` | `rc-shared: global` | `global` | так | 168 | 168 |
| `cpu.doinit`, `options = []option{…}` | `internal/cpu/cpu_x86.go:73` | `rc-shared: global` | `global` | так | 256 | 256 |
| `unicode.init`, `GraphicRanges` | `unicode/graphic.go:23` | `rc-shared: global` | `global` | так | 48 | 48 |
| `unicode.init`, `PrintRanges` | `unicode/graphic.go:29` | `rc-shared: global` | `global` | так | 40 | 40 |
| `syscall.init`, `mapper = &mmapper{…}` | `syscall/syscall_linux.go:1272` | `rc-shared: global` | `global` | так | 48 | 48 |
| **Разом** | | | | | **33 392** | **524 912** |

P8.8: `10` — 608 + 32 784 = 33 392; `14` — 608 + 524 304 = 524 912. Вузли `NewTree` мають позначку свого місця (`cyclic: return`, свіжий результат); живі вони лише тому, що `longLivedTree` — `cyclic: go` і `DecCyclic` не отримує. Це 2^(n+1)−1 вузлів по 16 байтів.

### binarytreesarena `10`

| Функція | Рядок | v1 | Шлях | Глобальна | Байти |
| --- | --- | --- | --- | --- | ---: |
| `(*WaitGroup).Go`, параметр `f` | `sync/waitgroup.go:236` | `rc-shared: go` | `go` | ні | 160 (10 × 16) |
| `(*WaitGroup).Go`, отримувач `wg` | `sync/waitgroup.go:236` | `rc-shared: go` | `go` | ні | 80 (10 × 8) |
| `run`, `maxDepth` | `main.go:65` | `rc-shared: move` | `move → go` | ні | 8 |
| `run`, `var stretchCheck int` | `main.go:72` | `rc-shared: move` | `move → go` | ні | 8 |
| `run`, `longLived` (комірка змінної) | `main.go:78` | `rc-shared: move` | `move → go` | ні | 24 |
| `run`, `var chunks []chunk` (комірка) | `main.go:85` | `rc-shared: move` | `move → go` | ні | 24 |
| `run`, `checks` (комірка) | `main.go:94` | `rc-shared: move` | `move → go` | ні | 24 |
| `run`, `var next atomic.Int64` | `main.go:95` | `rc-shared: move` | `move → go` | ні | 8 |
| стандартні бібліотеки (`ffi`, `cpu`, `unicode`, `syscall`), як у binarytrees | | `global` | `global` | так | 584 |
| **Разом** | | | | | **904** |

P8.8: 880 + 24 = 904. Тут 24 байти `cyclic` — `ffi.typeEmpty`.

### fannkuchredux `7`

| Функція | Рядок | v1 | Шлях | Глобальна | Байти |
| --- | --- | --- | --- | --- | ---: |
| `(*WaitGroup).Go`, параметр `f` | `sync/waitgroup.go:236` | `rc-shared: go` | `go` | ні | 128 (8 × 16) |
| `(*WaitGroup).Go`, отримувач `wg` | `sync/waitgroup.go:236` | `rc-shared: go` | `go` | ні | 64 (8 × 8) |
| `fannkuch`, параметр `n` | `main.go:97` | `rc-shared: move` | `move → go` | ні | 8 |
| `fannkuch`, `tasks` (комірка) | `main.go:102` | `rc-shared: move` | `move → go` | ні | 24 |
| `fannkuch`, `sums` (комірка) | `main.go:115` | `rc-shared: move` | `move → go` | ні | 24 |
| `fannkuch`, `flips` (комірка) | `main.go:116` | `rc-shared: move` | `move → go` | ні | 24 |
| `fannkuch`, `var next atomic.Int64` | `main.go:117` | `rc-shared: move` | `move → go` | ні | 8 |
| стандартні бібліотеки, як у binarytrees | | `global` | `global` | так | 584 |
| **Разом** | | | | | **864** |

P8.8: 840 + 24 = 864.

### Підсумок за шляхом

| Програма | `go` / `move → go` | `global`, досяжне на виході | `channel` | `global`, недосяжне |
| --- | ---: | ---: | ---: | ---: |
| binarytrees 10 | 32 808 | 584 | 0 | 0 |
| binarytrees 14 | 524 328 | 584 | 0 | 0 |
| binarytreesarena 10 | 320 | 584 | 0 | 0 |
| fannkuchredux 7 | 280 | 584 | 0 | 0 |

Байти `global` — шість блоків ініціалізації пакетів, однакові в кожній програмі; у `gc` вони теж живі до виходу. Решта — комірки змінних і параметрів, захоплені замиканням горутини, і дерево `longLivedTree`. Місця з `move` — захоплення замиканням, яке йде в `(*sync.WaitGroup).Go`: v1 бачить передачу аргументом, а `go` стоїть у викликаній функції.
