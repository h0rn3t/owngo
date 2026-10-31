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
