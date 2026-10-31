# Звіт фази 7

База фази — корпус `owngo/bench` (рішення розробника 2026-10-31, `PLAN.md`). Ківш — `unmatched`.

## P7.3 Профілі корпусу

Кожна Go-програма корпусу має `profile_test.go` з `TestProfile`, який кличе `main` з малим аргументом; файл програми не змінено. `owngo/bench/census.sh` знімає профіль `go test -run '^TestProfile$' -memprofilerate=1 -memprofile=…/mem.out` і проганяє `phase0 -dir <програма> -profdir <профіль>`. Скрипт звіряє суму категорій v1 і `unmatched` із сумою `alloc_space` у `go tool pprof -raw`; для кожної програми вони рівні.

Debian 13 amd64, 8 ядер, Go 1.27.1, ревізія `b781a392c`. `owngo/bench/census.sh` — код 0, 8.8 с.

| Програма | Аргумент | Категорія v1 | Місць | Об'єкти | Байти | Частка байтів |
| --- | ---: | --- | ---: | ---: | ---: | ---: |
| binarytrees | 10 | rc-local | 12 | 8 | 128 | 0% |
| binarytrees | 10 | rc-shared | 7 | 12 | 576 | 0% |
| binarytrees | 10 | cyclic | 3 | 135 855 | 2 173 672 | 97.8% |
| binarytrees | 10 | **unmatched** | 0 | 187 | 47 632 | 2.1% |
| binarytreesarena | 10 | stack | 4 | 8 | 1 364 | 0.6% |
| binarytreesarena | 10 | static | 1 | 0 | 1 360 | 0.6% |
| binarytreesarena | 10 | rc-local | 25 | 45 | 184 084 | 82.3% |
| binarytreesarena | 10 | **unmatched** | 0 | 154 | 36 824 | 16.5% |
| fannkuchredux | 7 | stack | 2 | 0 | 0 | 0% |
| fannkuchredux | 7 | static | 2 | 0 | 0 | 0% |
| fannkuchredux | 7 | rc-local | 17 | 28 | 4 032 | 11.9% |
| fannkuchredux | 7 | **unmatched** | 0 | 145 | 29 888 | 88.1% |

`unmatched` кожної програми:

| Програма | Об'єкти | Байти | Частка байтів профілю |
| --- | ---: | ---: | ---: |
| binarytrees | 187 | 47 632 | 2.1% |
| binarytreesarena | 154 | 36 824 | 16.5% |
| fannkuchredux | 145 | 29 888 | 88.1% |

Абсолютний `unmatched` у трьох програм близький (29–48 КБ, 145–187 об'єктів), а частка залежить від того, скільки алокує сама програма. Профіль знято тестом, тож у ньому є і обв'язка `testing`, якої в бінарнику програми немає. Розклад за причиною — P7.4.

Рядки `stack` і `static` арени — наслідок правила `Weigh`: місця на одному рядку ділять вибірку порівну, остача дістається першому. Один об'єкт на 2 724 байти ділиться між місцем `stack` і місцем `static` одного рядка: об'єкт іде першому, байти — 1 364 і 1 360.

Пробний замір 2026-10-31 на binary-trees `10` дав `rc-local` 176 і `unmatched` 41 832 байти (128 об'єктів). Той тест у репозиторій не потрапив. `cyclic` і `rc-shared` збігаються з P7.3, `rc-local` і `unmatched` — ні.

## P7.4 Причини `unmatched`

`census.UnmatchedReasons` розкладає вибірки, які `Weigh` лишає без місця v1. Причину дає перший кадр поза runtime, як в `Outside`. Виняток — обв'язка тесту (`harness`): її визначає весь стек. Вибірка — обв'язка, якщо стек проходить через пакет `testing`, `_testmain.go` або `*_test.go` і не заходить у жоден нетестовий файл програми. Тож `flag` чи `os`, які кличе лише `testing`, теж обв'язка. Решта причин: `runtime` — у стеку лише runtime; `stdlib`; `external` — модуль поза `-dir`; `module` — рядок файлу програми без місця, з функцією і рядком. Рядок `stdlib` `phase0` з'єднує з місцями v1 пакетів stdlib, через кадри яких проходять його вибірки, як `StdlibCategories`. `phase0` пише розклад у JSON як `unmatched_v1`, `census.sh` звіряє суму причин із рядком `unmatched` і друкує таблиці.

Ревізія `e481ca002`, та сама машина, `owngo/bench/census.sh` — код 0. Числа runtime і stdlib між прогонами трохи різняться (кількість M і G у runtime `gc`, планування горутин); `cyclic` і `rc-local` програм — ні.

Обв'язка тесту окремо — у бінарник програми вона не входить:

| Програма | Об'єкти | Байти | Частка профілю |
| --- | ---: | ---: | ---: |
| binarytrees | 110 | 10 696 | 0.5% |
| binarytreesarena | 111 | 10 728 | 4.7% |
| fannkuchredux | 112 | 10 744 | 27.3% |

Причини програми, без обв'язки:

| Програма | Причина | Об'єкти | Байти | Частка профілю |
| --- | --- | ---: | ---: | ---: |
| binarytrees | runtime | 54 | 39 368 | 1.8% |
| binarytrees | stdlib | 23 | 2 592 | 0.1% |
| binarytrees | module | 1 | 16 | 0% |
| binarytreesarena | runtime | 47 | 30 760 | 13.4% |
| binarytreesarena | stdlib | 4 | 784 | 0.3% |
| fannkuchredux | runtime | 39 | 24 480 | 62.2% |
| fannkuchredux | stdlib | 1 | 112 | 0.3% |

`external` немає: корпус не має залежностей поза stdlib. Сума причин кожної програми дорівнює рядку `unmatched` (52 672, 42 272 і 35 336 байтів).

Рядок `stdlib` за місцями v1 пакетів кадрів (рядки з нулями пропущено):

| Програма | Категорія v1 | Місць | Об'єкти | Байти |
| --- | --- | ---: | ---: | ---: |
| binarytrees | static | 134 | 11 | 328 |
| binarytrees | rc-local | 623 | 6 | 288 |
| binarytrees | rc-shared | 42 | 0 | 8 |
| binarytrees | pinned | 2 | 1 | 1 152 |
| binarytrees | unmatched | 0 | 5 | 816 |
| binarytreesarena | rc-local | 623 | 3 | 672 |
| binarytreesarena | unmatched | 0 | 1 | 112 |
| fannkuchredux | unmatched | 0 | 1 | 112 |

Рядок `module` — одна вибірка: `binarytrees.Run.func3`, `binarytrees/main.go:125`, 1 об'єкт, 16 байтів. Це аргумент `iterations` у `fmt.Sprintf(...)`, який упаковується в інтерфейс (`runtime.convT64`). Вибірка стоїть на рядку аргументу, місце census на цьому рядку не стоїть. В іншому прогоні — 2 об'єкти, 32 байти.

Найбільша причина поза обв'язкою в кожній програмі — голий `runtime`. Найбільші стеки — структури самого runtime `gc`: `runtime.allocm` (M), `runtime.malg` (G), `runtime.makeProfStackFP` (стеки профілювальника), `runtime.gcBgMarkWorker`. У бінарнику llgo runtime інший, тож це вибірки інструмента виміру, а не алокації програми.
