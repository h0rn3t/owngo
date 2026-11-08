# fiberdb — стенд горутин (P-NEW-09.1)

HTTP-сервіс на fiber v3 і SQLite (`mattn/go-sqlite3`, cgo). Окремий модуль: його залежності не потрапляють у `go.mod` llgo, а `go test ./...` з кореня його не бачить.

| Маршрут | Що робить |
| --- | --- |
| `POST /items` `{"name","qty"}` | вставка, 201 з `id` |
| `GET /items/:id` | 200, 404, 400 на нечисловий `id` |
| `GET /items?after=&limit=` | сторінка за `id` (keyset), `limit` 1–1000, порожня — `[]` |
| `GET /fanout?n=N` | N горутин, живих одночасно (кожна чекає, доки стартують усі), потім кожна читає `count(*)`; відповідь `{"n","peak","rows"}`, `peak` — `runtime.NumGoroutine()` у момент, коли живі всі N; N — 1…100 000 |

Версії: fiber v3.4.0 і `gofiber/utils` v2.1.1. `utils` v2.4+ (fiber v3.5+) має асемблер AVX2 з локальним `#include`. Транслятор plan9asm llgo такий `#include` пропускає і не знає `VPSUBB`, `VPADDB`, `VPMINUB`, `VPSADBW`, `VPSRLW`, `VPINSRQ`, `VEXTRACTI128`, `VMOVD`, `VMOVQ`.

## Запуск

З кореня репозиторію, LLVM 23 першим у `PATH`, `LLGO_ROOT` — корінь:

```sh
cd owngo/stand/fiberdb
go test -race .              # контракт API під gc
llgo test .                  # те саме під llgo (1:1)
go build -o $BIN/fiberdb-go . && go build -o $BIN/load ./load
llgo build -o $BIN/fiberdb-llgo .
BIN=$BIN ./measure.sh go 18080      # fanout 1000/5000/20000, навантаження c=100 і c=1000
BIN=$BIN ./measure.sh llgo 18081
```

`load` — `-url`, `-c` (клієнтів, кожен на своєму з'єднанні), `-d` (тривалість); друкує rps, p50, p99, max і помилки.

Під `-tags owngo` стенд поки не збирається — задача P-NEW-09.2 у `PLAN.md`. Заміри — `owngo/REPORT-STAGEG.md`.
