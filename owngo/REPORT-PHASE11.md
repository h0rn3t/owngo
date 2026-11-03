# OwnGo — звіт фази 11

## P11.3 `GOROOT/test` з тегом `owngo` проти базового llgo

Режим `ci` (`.`, `fixedbugs`, 830 випадків), Go 1.27.1, `linux/amd64`, LLVM 23. Раннер `test/goroot` з `-min-swap-free-mib 0` (на машині розробки вільного свопу ~30 МіБ); ліміт RSS раннера — типовий `-max-rss-mib 4096`. ASan — `-llgo-flags "-tags owngo -O0 -asan"`, `ASAN_OPTIONS=detect_leaks=0`.

### Повні прогони

| SHA | Варіант | `pass` | Регресій проти без тегу |
| --- | --- | --- | --- |
| `192718a3e` | без тегу | 806 | — |
| `192718a3e` | `owngo` | 746 | 60 |
| `192718a3e` | `owngo -O0 -asan` | 735 | 71 (61 `heap-use-after-free`) |
| `f7dddb9bd` | без тегу | 806 | — |
| `f7dddb9bd` | `owngo` | 796 | 10 |
| `f7dddb9bd` | `owngo -O0 -asan` | 796 | 10, жодного `heap-use-after-free`, `double-free` чи `attempting free` |

Без тегу на обох SHA падає `fixedbugs/issue14636.go` і є 1 `XFAIL` — це не регресії `owngo`.

### Підсумковий перезапуск

На `be0894d2f` перезапущено 15 випадків: усі 10 регресій `f7dddb9bd` кожного варіанта. П'ять із них були регресіями лише без ASan, тож обидва списки разом дають 15 випадків. Після `be0894d2f` код ще змінився: `7327b1bb6` прибирає перевірку на `nil` для запису нульового розміру в щойно виділену пам'ять. Поведінки програм ця зміна не змінює.

| Випадок | Без тегу | `owngo` | `owngo -O0 -asan` | Виправлено |
| --- | --- | --- | --- | --- |
| `fixedbugs/bug102.go`, `issue13162.go`, `issue23536.go`, `issue23814.go`, `named.go` | `pass` | `pass` | `pass` | `d339c8237` — `FreeRoot(nil)` |
| `fixedbugs/issue41239.go`, `issue7550.go` | `pass` | `pass` | `pass` | `b4ce48a34` — ємність owned `append` |
| `recover.go` | `pass` | `pass` | `pass` | `b21c0f3eb` — поле нульового розміру у вузлі `defer` |
| `init1.go`, `mallocfin.go`, `tinyfin.go` | `pass` | `unexpected-fail` | `unexpected-fail` | не застосовно, див. нижче |
| `fixedbugs/issue78081.go` | `pass` | `pass` | `resource-fail` | обмеження, див. нижче |
| `inline_callers.go`, `peano.go` | `pass` | `pass` | `unexpected-fail` | обмеження ASan / `-O0` |
| `rangegen.go` | `pass` | `pass` | `resource-fail` | обмеження ASan / `-O0` |

Регресій без причини немає: з `owngo` — 3 випадки, під ASan — 7, і для кожного причина в списку нижче.

### Список з причинами

- **Не застосовно (рішення розробника, 2026-11-03).** `init1.go` вимагає, щоб `runtime.NumGC` зростав. `mallocfin.go` і `tinyfin.go` чекають виклику фіналізаторів `runtime.SetFinalizer`. Під `owngo` трасувального збирача немає за задумом, тож лічильника GC і фіналізаторів теж немає.
- **`fixedbugs/issue78081.go` під ASan — ліміт пам'яті раннера (рішення розробника, 2026-11-03).** 16 горутин по 100 000 разів виконують `*sink = make([]byte, 1024)`. Прохід не звільняє попередній буфер при записі вказівника в пам'ять: таке правило фази 5 у `PLAN.md` («Фаза це правило не послаблює»). Без ASan `owngo live rc-local` на виході показує 1 638 402 272 байти, і випадок проходить. Під ASan із його накладними витратами пам'ять перевищує 4 ГіБ. Звільнення старого значення при перезаписі — можливий наступний крок моделі, не дефект P11.3.
- **Обмеження ASan / `-O0`.** `inline_callers.go`, `peano.go`, `rangegen.go` падають під `-O0 -asan` і без тегу `owngo` (перевірено 2026-11-03), тож це не модель пам'яті. `inline_callers.go` — інші кадри `runtime.Callers` (`main.h main.h main.g` замість `main.h main.g main.f`); `peano.go` — глибока рекурсія, процес завершується з кодом -1; `rangegen.go` — збірка згенерованої програми перевищує ліміт RSS 4 ГіБ.
