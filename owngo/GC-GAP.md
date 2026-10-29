# Розрив go/ssa і gc — P0.7

Місце вважається розривом, коли census ставить йому категорію іншу за `stack` (go/ssa тримає об'єкт у купі), а `gc` 1.27.3 на тому самому `file:line` пише, що алокація не тікає, і на рядку немає діагностики втечі в купу.

Команда збору: `go build -gcflags=-m -json=0,file://…`. JSON (коди `escape` і `escapes`) містить лише втечі в купу. Рядки «does not escape» дає лише `-m`. Голий ідентифікатор (`b does not escape`) — це параметр або змінна, не алокація, і розриву не утворює. `append` і вирази (`new`, `make`, `&T{}`, `func literal`, `b.N`, конкатенація) утворюють. Рядок, де одночасно є «does not escape» для алокації і «escapes to heap» або «moved to heap», розривом не є.

`make(chan …)` gc не діагностує і завжди виділяє в купі через `runtime.makechan`, тож такі місця сюди не входять. `make` зі змінним розміром із «does not escape» лишається на стеку лише до порога компілятора (32 байти, `VariableMakeThreshold`); більший розмір усе одно йде в купу, і діагностика цього не розрізняє.

## Корпус `owngo/census/testdata`

73 місця census, з них 8 `stack` і 65 у купі за go/ssa. Розривів: **26** (40% місць поза `stack`).

| Категорія census | Розривів |
| --- | ---: |
| static | 12 |
| pinned (unsafe) | 6 |
| cyclic | 3 |
| implicit: append | 2 |
| implicit: concat | 1 |
| rc-local: call | 1 |
| rc-local: closure | 1 |

Типовий розрив корпусу: `new(int)` і `make` без втечі з функції. У go/ssa `new` завжди `Heap == true`, тож census дає `static`; gc кладе значення на стек. Канал `make(chan int)` у `static.go` розривом не є. Повернення вказівника (`rclocal.go:4`) теж не є: обидва компілятори кладуть його в купу.

Список зафіксовано в `TestCorpusGCGap`.

## Сервіс ms-faq

[ms-faq](https://gitlab.datahub.ua.energy/tko/ms-faq) `develop` @ `520cbc6`, модуль `ms-faq`, Go 1.27. Пакети `./...`: `cmd/app` і одинадцять пакетів під `internal/`, включно з `internal/testutil` (звичайний пакет, не `*_test.go`).

701 місце census: 83 `stack`, 618 у купі за go/ssa. Розривів: **45** (7% місць поза `stack`). З них 14 у `internal/testutil`, 31 у решті модуля.

| Категорія census | Місць | Розривів |
| --- | ---: | ---: |
| stack | 83 | 0 |
| static | 42 | 12 |
| rc-local | 518 | 30 |
| rc-shared | 6 | 0 |
| cyclic | 9 | 0 |
| implicit | 43 | 3 |
| pinned | 0 | 0 |

Причини розривів: `rc-local: call` 26, `static` 12, `rc-local: closure` 4, `implicit: append` 2, `implicit: string` 1.

Більшість купи сервісу — аргумент виклику (`rc-local: call`, 518 місць), і gc теж лишає його в купі. Розрив тут вужчий, ніж на корпусі: 30 із цих 518. Це тимчасові масиви аргументів (`*[2]*string` у `cmp.Or`, `*[n]any` у `Query`) і мапи на кшталт `url.Values{}`, які gc не виводить зі стека, а v0 будь-який виклик вважає збереженням. Замикання, яке gc не виводить (`func literal does not escape`), census усе одно бачить як об'єкт go/ssa в купі.

| Позиція | Категорія | Причина | Тип |
| --- | --- | --- | --- |
| `internal/config/config.go:308` | rc-local | call | `net/url.Values` |
| `internal/middleware/apm.go:83` | static | — | `func()` |
| `internal/middleware/apm.go:143` | rc-local | closure | `*net/http.Header` |
| `internal/middleware/apm.go:143` | rc-local | closure | `net/http.Header` |
| `internal/middleware/http_logging.go:46` | rc-local | call | `*[1]github.com/gofiber/fiber/v3/middleware/logger.Config` |
| `internal/middleware/http_logging.go:112` | implicit | string | `string` |
| `internal/schema/request.go:69` | rc-local | call | `*[2]*string` |
| `internal/schema/request.go:70` | rc-local | call | `*[2]*string` |
| `internal/schema/request.go:71` | rc-local | call | `*[2]*string` |
| `internal/schema/request.go:72` | rc-local | call | `*[2]*string` |
| `internal/schema/request.go:73` | rc-local | call | `*[2]*[]string` |
| `internal/schema/request.go:74` | rc-local | call | `*[2]*string` |
| `internal/integration/files/client.go:72` | rc-local | call | `net/url.Values` |
| `internal/integration/files/client.go:108` | static | — | `func()` |
| `internal/repository/faq.go:193` | rc-local | closure | `**database/sql.Rows` |
| `internal/repository/faq.go:201` | static | — | `func()` |
| `internal/repository/faq.go:232` | rc-local | call | `*[5]any` |
| `internal/repository/faq.go:318` | rc-local | call | `*[7]any` |
| `internal/repository/instructions.go:91` | rc-local | call | `func(m ms-faq/internal/model.MsFaqInstruction) bool` |
| `internal/repository/instructions.go:91` | rc-local | call | `*ms-faq/internal/model.MsFaqInstruction` |
| `internal/repository/instructions.go:127` | rc-local | closure | `**database/sql.Rows` |
| `internal/repository/instructions.go:134` | static | — | `func()` |
| `internal/repository/instructions.go:162` | rc-local | call | `*[7]any` |
| `internal/repository/instructions.go:182` | static | — | `map[string]*ms-faq/internal/schema.ChapterGroup` |
| `internal/repository/instructions.go:183` | static | — | `*[0]string` |
| `internal/repository/instructions.go:189` | static | — | `*[1]string` |
| `internal/repository/instructions.go:189` | implicit | append | `[]string` |
| `internal/handler/instructions.go:57` | static | — | `func()` |
| `internal/handler/instructions.go:160` | static | — | `func()` |
| `internal/handler/router.go:51` | rc-local | call | `*[1]any` |
| `cmd/app/main.go:88` | rc-local | call | `*[2]os.Signal` |
| `internal/testutil/db.go:20` | rc-local | call | `*[1]any` |
| `internal/testutil/db.go:25` | rc-local | call | `*[1]any` |
| `internal/testutil/db.go:30` | rc-local | call | `*[1]any` |
| `internal/testutil/db.go:34` | rc-local | call | `*[1]any` |
| `internal/testutil/env.go:21` | static | — | `*[2]string` |
| `internal/testutil/env.go:22` | rc-local | call | `*[4]string` |
| `internal/testutil/env.go:23` | rc-local | call | `*[2]string` |
| `internal/testutil/env.go:26` | static | — | `*[1]string` |
| `internal/testutil/env.go:26` | implicit | append | `[]string` |
| `internal/testutil/env.go:45` | rc-local | call | `*[2]string` |
| `internal/testutil/env.go:64` | static | — | `func()` |
| `internal/testutil/xuser.go:28` | rc-local | call | `*[1]any` |
| `internal/testutil/xuser.go:35` | rc-local | call | `*[1]any` |
| `internal/testutil/xuser.go:39` | rc-local | call | `*[1]any` |

