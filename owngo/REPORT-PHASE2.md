# Звіт фази 2

Профілі й місця census — ті самі, що в P1.1: юніт-тести `go test -count=1 -memprofilerate=1`, без `//go:build integration` і без окремого прогону бенчмарків. `ms-customers` профілю не має. Рядок stdlib — 2 418 622 160 байтів на 17 сервісах і 114 923 904 у `ms-faq`.

## P2.1 Пакети стандартної бібліотеки

`StdlibPackages` бере вибірку, яку `Outside` кладе в stdlib, і називає її імпортом пакета першого кадру під `GOROOT/src`. Кадр пакета `runtime` провалюється, як у `Outside`. Ім'я файлу в пакет не входить: `encoding/json/encode.go` — це `encoding/json`. Каталог `vendor` лишається в шляху: `vendor/golang.org/x/net/http2/hpack`. Відсоток ділить на байти цього рядка stdlib. Округлення до одного знака, як у `FormatWeights`.

Байтів stdlib по сервісах ті самі, що в таблиці P1.1. Сума пакетів дорівнює рядку.

| Сервіс | Байти stdlib |
| --- | ---: |
| ms-zv | 141820632 |
| ms-ap | 47323224 |
| ms-notifications | 43707408 |
| ms-users | 36771672 |
| ms-gateway | 174013784 |
| ms-export | 811543680 |
| ms-reference-book | 32252848 |
| ms-files | 181253248 |
| ms-events | 11437896 |
| ms-search | 179382360 |
| ms-companies | 108796920 |
| ms-auth | 4908456 |
| ms-ved | 479064176 |
| ms-converter | 124865288 |
| ms-public | 12321968 |
| ms-settings | 16651272 |
| ms-sync | 12507328 |
| 17 сервісів | 2418622160 |
| ms-faq | 114923904 |

На 17 сервісах 107 пакетів, 17 955 703 об'єктів і 2 418 622 160 байтів. Найбільші — `compress/flate` (767 319 808, 31.7%) і `encoding/xml` (601 399 088, 24.9%). Разом 1 368 718 896 байтів, 56.6% рядка.

| Пакет | Об'єкти | % об'єктів | Байти | % байтів |
| --- | ---: | ---: | ---: | ---: |
| compress/flate | 30600 | 0.2 | 767319808 | 31.7 |
| encoding/xml | 11337812 | 63.1 | 601399088 | 24.9 |
| reflect | 2903247 | 16.2 | 276621272 | 11.4 |
| bytes | 196793 | 1.1 | 191247024 | 7.9 |
| io | 33746 | 0.2 | 163472680 | 6.8 |
| bufio | 35104 | 0.2 | 75072224 | 3.1 |
| internal/sync | 350695 | 2.0 | 51073872 | 2.1 |
| time | 557251 | 3.1 | 43100072 | 1.8 |
| regexp | 62143 | 0.3 | 31618448 | 1.3 |
| net/http | 215294 | 1.2 | 28317808 | 1.2 |
| testing | 206827 | 1.2 | 25656616 | 1.1 |
| sync | 24038 | 0.1 | 20360496 | 0.8 |
| regexp/syntax | 37928 | 0.2 | 15191952 | 0.6 |
| strings | 309521 | 1.7 | 14790856 | 0.6 |
| strconv | 269826 | 1.5 | 12959744 | 0.5 |
| encoding/json/v2 | 66852 | 0.4 | 10900904 | 0.5 |
| net/textproto | 96933 | 0.5 | 9354208 | 0.4 |
| slices | 62741 | 0.3 | 8013904 | 0.3 |
| context | 110717 | 0.6 | 7995680 | 0.3 |
| fmt | 187028 | 1.0 | 6681208 | 0.3 |
| encoding/json/jsontext | 17696 | 0.1 | 6651744 | 0.3 |
| internal/routebsd | 75297 | 0.4 | 5984664 | 0.2 |
| net | 106805 | 0.6 | 5628744 | 0.2 |
| archive/zip | 75410 | 0.4 | 4596520 | 0.2 |
| internal/stringslite | 236308 | 1.3 | 4116880 | 0.2 |
| syscall | 28400 | 0.2 | 4060624 | 0.2 |
| os | 6426 | 0.0 | 3305712 | 0.1 |
| internal/strconv | 29414 | 0.2 | 2835816 | 0.1 |
| database/sql | 32837 | 0.2 | 2807352 | 0.1 |
| text/template/parse | 36154 | 0.2 | 2128240 | 0.1 |
| math/rand | 546 | 0.0 | 2020032 | 0.1 |
| net/url | 15194 | 0.1 | 1976392 | 0.1 |
| net/http/httptest | 16380 | 0.1 | 1949528 | 0.1 |
| internal/reflectlite | 34603 | 0.2 | 1118192 | 0.0 |
| runtime/pprof | 37162 | 0.2 | 1009768 | 0.0 |
| unicode/utf8 | 29044 | 0.2 | 800400 | 0.0 |
| encoding/base64 | 1729 | 0.0 | 728848 | 0.0 |
| log/slog/internal/buffer | 1255 | 0.0 | 635192 | 0.0 |
| embed | 72 | 0.0 | 518976 | 0.0 |
| mime | 18199 | 0.1 | 502216 | 0.0 |
| crypto/internal/fips140/sha256 | 4673 | 0.0 | 429760 | 0.0 |
| crypto/internal/fips140/hmac | 4379 | 0.0 | 326912 | 0.0 |
| maps | 1296 | 0.0 | 271200 | 0.0 |
| text/template | 3598 | 0.0 | 232096 | 0.0 |
| crypto/x509 | 1889 | 0.0 | 215232 | 0.0 |
| container/list | 3196 | 0.0 | 153408 | 0.0 |
| net/http/httputil | 5853 | 0.0 | 153232 | 0.0 |
| compress/gzip | 472 | 0.0 | 151680 | 0.0 |
| crypto/internal/fips140/bigmod | 1147 | 0.0 | 147312 | 0.0 |
| log/slog | 2331 | 0.0 | 142128 | 0.0 |
| math/big | 1254 | 0.0 | 138704 | 0.0 |
| hash/crc32 | 8655 | 0.0 | 138480 | 0.0 |
| mime/multipart | 2439 | 0.0 | 127584 | 0.0 |
| log | 1700 | 0.0 | 122992 | 0.0 |
| errors | 2333 | 0.0 | 107560 | 0.0 |
| encoding/json | 2399 | 0.0 | 99240 | 0.0 |
| io/fs | 6 | 0.0 | 92672 | 0.0 |
| internal/nettest | 846 | 0.0 | 85880 | 0.0 |
| internal/abi | 1799 | 0.0 | 85688 | 0.0 |
| net/http/internal/http2 | 1774 | 0.0 | 81312 | 0.0 |
| encoding/hex | 1310 | 0.0 | 80848 | 0.0 |
| flag | 3 | 0.0 | 76416 | 0.0 |
| encoding/pem | 230 | 0.0 | 74112 | 0.0 |
| internal/fmtsort | 427 | 0.0 | 47328 | 0.0 |
| os/user | 75 | 0.0 | 46160 | 0.0 |
| compress/zlib | 434 | 0.0 | 39680 | 0.0 |
| vendor/golang.org/x/net/http2/hpack | 10 | 0.0 | 39424 | 0.0 |
| crypto/rsa | 217 | 0.0 | 35712 | 0.0 |
| vendor/golang.org/x/crypto/cryptobyte | 620 | 0.0 | 28768 | 0.0 |
| crypto/sha1 | 383 | 0.0 | 26176 | 0.0 |
| net/netip | 1332 | 0.0 | 24416 | 0.0 |
| text/tabwriter | 98 | 0.0 | 22816 | 0.0 |
| encoding/json/internal/jsonwire | 225 | 0.0 | 21888 | 0.0 |
| os/exec | 69 | 0.0 | 17904 | 0.0 |
| internal/filepathlite | 497 | 0.0 | 16600 | 0.0 |
| internal/poll | 217 | 0.0 | 15576 | 0.0 |
| internal/gate | 139 | 0.0 | 15568 | 0.0 |
| unicode | 3 | 0.0 | 14848 | 0.0 |
| encoding/binary | 599 | 0.0 | 14704 | 0.0 |
| internal/godebug | 458 | 0.0 | 14656 | 0.0 |
| internal/singleflight | 187 | 0.0 | 12864 | 0.0 |
| runtime/debug | 12 | 0.0 | 12816 | 0.0 |
| crypto/tls | 82 | 0.0 | 11088 | 0.0 |
| path/filepath | 61 | 0.0 | 9440 | 0.0 |
| encoding/asn1 | 248 | 0.0 | 9424 | 0.0 |
| vendor/golang.org/x/net/http/httpproxy | 129 | 0.0 | 8944 | 0.0 |
| net/http/cookiejar | 184 | 0.0 | 8832 | 0.0 |
| testing/synctest | 123 | 0.0 | 8752 | 0.0 |
| crypto/x509/pkix | 248 | 0.0 | 7936 | 0.0 |
| sort | 468 | 0.0 | 7488 | 0.0 |
| crypto/internal/fips140/rsa | 32 | 0.0 | 5520 | 0.0 |
| net/http/pprof | 52 | 0.0 | 4064 | 0.0 |
| net/mail | 135 | 0.0 | 3736 | 0.0 |
| crypto/rand | 80 | 0.0 | 2560 | 0.0 |
| hash/adler32 | 115 | 0.0 | 1840 | 0.0 |
| html/template | 1 | 0.0 | 896 | 0.0 |
| vendor/golang.org/x/net/dns/dnsmessage | 1 | 0.0 | 896 | 0.0 |
| database/sql/driver | 36 | 0.0 | 816 | 0.0 |
| net/http/internal | 17 | 0.0 | 816 | 0.0 |
| encoding/gob | 4 | 0.0 | 680 | 0.0 |
| net/smtp | 13 | 0.0 | 616 | 0.0 |
| internal/syscall/unix | 30 | 0.0 | 560 | 0.0 |
| iter | 14 | 0.0 | 448 | 0.0 |
| encoding/base32 | 11 | 0.0 | 352 | 0.0 |
| crypto/md5 | 6 | 0.0 | 336 | 0.0 |
| crypto/internal/fips140hash | 1 | 0.0 | 48 | 0.0 |
| testing/iotest | 1 | 0.0 | 16 | 0.0 |
| разом | 17955703 | 100.0 | 2418622160 | 100.0 |

У `ms-faq` 79 пакетів, 63 804 об'єктів і 114 923 904 байти. `compress/flate` тримає 96 113 456 байтів, 83.6% рядка.

| Пакет | Об'єкти | % об'єктів | Байти | % байтів |
| --- | ---: | ---: | ---: | ---: |
| compress/flate | 1591 | 2.5 | 96113456 | 83.6 |
| bufio | 1356 | 2.1 | 7232448 | 6.3 |
| testing | 14791 | 23.2 | 2023832 | 1.8 |
| regexp | 408 | 0.6 | 1642344 | 1.4 |
| sync | 1256 | 2.0 | 1417392 | 1.2 |
| encoding/json/v2 | 4802 | 7.5 | 868272 | 0.8 |
| net/http | 2374 | 3.7 | 839296 | 0.7 |
| bytes | 2030 | 3.2 | 645496 | 0.6 |
| io | 949 | 1.5 | 562656 | 0.5 |
| internal/routebsd | 5555 | 8.7 | 420640 | 0.4 |
| reflect | 2207 | 3.5 | 332888 | 0.3 |
| syscall | 2611 | 4.1 | 323608 | 0.3 |
| encoding/json/jsontext | 1442 | 2.3 | 294352 | 0.3 |
| net/textproto | 2997 | 4.7 | 255152 | 0.2 |
| encoding/base64 | 537 | 0.8 | 176336 | 0.2 |
| strings | 1180 | 1.8 | 159432 | 0.1 |
| net | 2833 | 4.4 | 139344 | 0.1 |
| regexp/syntax | 37 | 0.1 | 124504 | 0.1 |
| os | 654 | 1.0 | 112824 | 0.1 |
| context | 1899 | 3.0 | 110016 | 0.1 |
| fmt | 871 | 1.4 | 99240 | 0.1 |
| internal/sync | 1412 | 2.2 | 98640 | 0.1 |
| encoding/xml | 2358 | 3.7 | 90144 | 0.1 |
| math/rand | 18 | 0.0 | 86176 | 0.1 |
| slices | 1035 | 1.6 | 71072 | 0.1 |
| time | 609 | 1.0 | 69040 | 0.1 |
| compress/gzip | 157 | 0.2 | 67440 | 0.1 |
| crypto/tls | 233 | 0.4 | 60592 | 0.1 |
| log/slog/internal/buffer | 98 | 0.2 | 51352 | 0.0 |
| net/http/httptest | 482 | 0.8 | 49584 | 0.0 |
| crypto/internal/fips140/mlkem | 14 | 0.0 | 47488 | 0.0 |
| mime/multipart | 1010 | 1.6 | 45408 | 0.0 |
| net/url | 418 | 0.7 | 45192 | 0.0 |
| vendor/golang.org/x/crypto/cryptobyte | 268 | 0.4 | 39168 | 0.0 |
| crypto/internal/fips140/sha256 | 304 | 0.5 | 27392 | 0.0 |
| log | 287 | 0.4 | 23256 | 0.0 |
| mime | 120 | 0.2 | 20160 | 0.0 |
| net/http/httputil | 684 | 1.1 | 17952 | 0.0 |
| crypto/internal/fips140/hmac | 231 | 0.4 | 17248 | 0.0 |
| crypto/internal/fips140/aes/gcm | 14 | 0.0 | 11840 | 0.0 |
| crypto/x509 | 89 | 0.1 | 10024 | 0.0 |
| internal/reflectlite | 242 | 0.4 | 8032 | 0.0 |
| encoding/json | 258 | 0.4 | 7184 | 0.0 |
| crypto/internal/fips140/aes | 13 | 0.0 | 6656 | 0.0 |
| crypto/internal/fips140/bigmod | 45 | 0.1 | 6112 | 0.0 |
| os/exec | 21 | 0.0 | 5744 | 0.0 |
| math/big | 51 | 0.1 | 5392 | 0.0 |
| database/sql | 46 | 0.1 | 4624 | 0.0 |
| crypto/internal/fips140/tls13 | 81 | 0.1 | 4288 | 0.0 |
| crypto/internal/fips140/hkdf | 125 | 0.2 | 3936 | 0.0 |
| internal/strconv | 134 | 0.2 | 3776 | 0.0 |
| log/slog | 71 | 0.1 | 3568 | 0.0 |
| encoding/pem | 7 | 0.0 | 2384 | 0.0 |
| internal/abi | 101 | 0.2 | 2360 | 0.0 |
| compress/zlib | 25 | 0.0 | 2240 | 0.0 |
| container/list | 41 | 0.1 | 1968 | 0.0 |
| internal/godebug | 60 | 0.1 | 1920 | 0.0 |
| crypto/ecdh | 28 | 0.0 | 1728 | 0.0 |
| net/http/internal/http2 | 34 | 0.1 | 1584 | 0.0 |
| crypto/rsa | 8 | 0.0 | 1168 | 0.0 |
| encoding/binary | 51 | 0.1 | 1104 | 0.0 |
| maps | 8 | 0.0 | 1024 | 0.0 |
| crypto/internal/fips140/rsa | 8 | 0.0 | 736 | 0.0 |
| strconv | 14 | 0.0 | 672 | 0.0 |
| internal/poll | 10 | 0.0 | 608 | 0.0 |
| errors | 27 | 0.0 | 512 | 0.0 |
| net/netip | 24 | 0.0 | 384 | 0.0 |
| crypto/x509/pkix | 12 | 0.0 | 384 | 0.0 |
| encoding/asn1 | 8 | 0.0 | 304 | 0.0 |
| vendor/golang.org/x/net/http/httpproxy | 3 | 0.0 | 208 | 0.0 |
| encoding/json/internal/jsonwire | 3 | 0.0 | 144 | 0.0 |
| crypto/internal/fips140/sha3 | 2 | 0.0 | 128 | 0.0 |
| hash/adler32 | 7 | 0.0 | 112 | 0.0 |
| crypto/mlkem | 6 | 0.0 | 48 | 0.0 |
| internal/stringslite | 3 | 0.0 | 48 | 0.0 |
| unicode/utf8 | 3 | 0.0 | 48 | 0.0 |
| crypto/x509/internal/macos | 1 | 0.0 | 48 | 0.0 |
| iter | 1 | 0.0 | 16 | 0.0 |
| weak | 1 | 0.0 | 16 | 0.0 |
| разом | 63804 | 100.0 | 114923904 | 100.0 |

## P2.2 Модулі поза `-dir`

`ExternalModules` бере вибірку, яку `Outside` кладе в модулі поза `-dir`, і називає її шляхом модуля. З елемента `pkg/mod` знімається суфікс `@версія`: `github.com/foo/bar@v1.2.3` і `github.com/foo/bar@v1.2.4` — один модуль `github.com/foo/bar`. Шлях, з якого модуль не читається, лишається рядком `поза кешем` і входить у суму. Кадр пакета `runtime` провалюється, як у `Outside`. Літера `!` у каталозі кеша лишається в імені: знімається лише суфікс версії. Завантаження модулів з мережі не виконується. Відсоток ділить на байти цього рядка. Округлення до одного знака, як у `FormatWeights`.

Байтів поза `-dir` по сервісах ті самі, що в таблиці P1.1. Сума модулів дорівнює рядку.

| Сервіс | Байти поза `-dir` |
| --- | ---: |
| ms-zv | 22480840 |
| ms-ap | 29392232 |
| ms-notifications | 89601392 |
| ms-users | 557011728 |
| ms-gateway | 742442696 |
| ms-export | 241392096 |
| ms-reference-book | 18278416 |
| ms-files | 59750000 |
| ms-events | 25652288 |
| ms-search | 66219600 |
| ms-companies | 132297632 |
| ms-auth | 927504 |
| ms-ved | 47479752 |
| ms-converter | 128240824 |
| ms-public | 27875184 |
| ms-settings | 8477976 |
| ms-sync | 27818752 |
| 17 сервісів | 2225338912 |
| ms-faq | 44337552 |

На 17 сервісах 96 модулів, 6 868 063 об'єкти і 2 225 338 912 байтів. Найбільші — `gopkg.in/yaml.v3` (725 650 128, 32.6%), `golang.org/x/crypto` (524 339 152, 23.6%) і `go.elastic.co/apm/v2` (482 555 088, 21.7%). Разом 1 732 544 368 байтів, 77.9% рядка. `поза кешем` — 87 480 байтів, 0.0% рядка.

| Модуль | Об'єкти | % об'єктів | Байти | % байтів |
| --- | ---: | ---: | ---: | ---: |
| gopkg.in/yaml.v3 | 3472215 | 50.6 | 725650128 | 32.6 |
| golang.org/x/crypto | 466 | 0.0 | 524339152 | 23.6 |
| go.elastic.co/apm/v2 | 30192 | 0.4 | 482555088 | 21.7 |
| github.com/xuri/excelize/v2 | 899285 | 13.1 | 192681456 | 8.7 |
| github.com/go-pdf/fpdf | 89565 | 1.3 | 88464120 | 4.0 |
| github.com/elastic/go-sysinfo | 6272 | 0.1 | 51226880 | 2.3 |
| gorm.io/gorm | 845417 | 12.3 | 47301584 | 2.1 |
| github.com/xuri/nfp | 986938 | 14.4 | 37395744 | 1.7 |
| github.com/valyala/bytebufferpool | 1607 | 0.0 | 18224328 | 0.8 |
| github.com/gofiber/fiber/v3 | 125097 | 1.8 | 16384568 | 0.7 |
| github.com/carlos7ags/folio | 188014 | 2.7 | 14520408 | 0.7 |
| github.com/valyala/fasthttp | 21339 | 0.3 | 3398896 | 0.2 |
| go.temporal.io/sdk | 17156 | 0.2 | 2337584 | 0.1 |
| github.com/andybalholm/brotli | 24 | 0.0 | 2160384 | 0.1 |
| google.golang.org/protobuf | 4679 | 0.1 | 2106984 | 0.1 |
| github.com/!d!a!t!a-!d!o!g/go-sqlmock | 26102 | 0.4 | 2046640 | 0.1 |
| go.temporal.io/api | 279 | 0.0 | 1314088 | 0.1 |
| github.com/goccy/go-json | 9982 | 0.1 | 1178552 | 0.1 |
| github.com/stretchr/testify | 20291 | 0.3 | 1168272 | 0.1 |
| github.com/jackc/pgx/v5 | 3437 | 0.1 | 1119832 | 0.1 |
| go.elastic.co/fastjson | 2330 | 0.0 | 1084336 | 0.0 |
| gorm.io/driver/postgres | 12908 | 0.2 | 928856 | 0.0 |
| golang.org/x/text | 4792 | 0.1 | 854888 | 0.0 |
| github.com/mattn/go-sqlite3 | 17705 | 0.3 | 806384 | 0.0 |
| github.com/flosch/pongo2/v7 | 4605 | 0.1 | 675824 | 0.0 |
| github.com/ilyakaznacheev/cleanenv | 4859 | 0.1 | 624024 | 0.0 |
| go.yaml.in/yaml/v3 | 5476 | 0.1 | 588952 | 0.0 |
| howett.net/plist | 11592 | 0.2 | 494336 | 0.0 |
| github.com/go-playground/validator/v10 | 3166 | 0.0 | 425096 | 0.0 |
| github.com/go-resty/resty/v2 | 3352 | 0.0 | 374296 | 0.0 |
| gitlab.datahub.ua.energy/tko/dhcore-go/apicore | 6377 | 0.1 | 281568 | 0.0 |
| github.com/google/uuid | 8123 | 0.1 | 271152 | 0.0 |
| gorm.io/driver/sqlite | 3011 | 0.0 | 190160 | 0.0 |
| github.com/caarlos0/env/v11 | 597 | 0.0 | 188224 | 0.0 |
| github.com/wk8/go-ordered-map/v2 | 5467 | 0.1 | 183752 | 0.0 |
| github.com/caarlos0/env/v10 | 501 | 0.0 | 172944 | 0.0 |
| github.com/minio/minio-go/v7 | 2001 | 0.0 | 162392 | 0.0 |
| github.com/gofiber/schema | 1457 | 0.0 | 151800 | 0.0 |
| go.uber.org/goleak | 19 | 0.0 | 132320 | 0.0 |
| gitlab.datahub.ua.energy/tko/dhcore-go/ext/fiber/v3/xuser | 1026 | 0.0 | 116816 | 0.0 |
| gitlab.datahub.ua.energy/tko/dhcore-go/ext/time | 2846 | 0.0 | 96488 | 0.0 |
| golang.org/x/net | 8 | 0.0 | 92720 | 0.0 |
| github.com/bahlo/generic-list-go | 1858 | 0.0 | 87920 | 0.0 |
| поза | 3452 | 0.1 | 87480 | 0.0 |
| github.com/tiendc/go-deepcopy | 1634 | 0.0 | 67384 | 0.0 |
| github.com/jinzhu/now | 1212 | 0.0 | 64640 | 0.0 |
| golang.org/x/sys | 1558 | 0.0 | 56744 | 0.0 |
| github.com/redis/go-redis/v9 | 160 | 0.0 | 55152 | 0.0 |
| github.com/golang/mock | 666 | 0.0 | 51848 | 0.0 |
| github.com/pbnjay/grate | 795 | 0.0 | 42000 | 0.0 |
| google.golang.org/grpc | 657 | 0.0 | 30264 | 0.0 |
| gitlab.datahub.ua.energy/tko/dhcore-go/error-provider | 500 | 0.0 | 26984 | 0.0 |
| gitlab.datahub.ua.energy/tko/dhcore-go/ext/fiber/v3/error-provider | 740 | 0.0 | 26840 | 0.0 |
| go.elastic.co/apm/module/apmhttp/v2 | 279 | 0.0 | 24952 | 0.0 |
| github.com/tinylib/msgp | 70 | 0.0 | 22400 | 0.0 |
| golang.org/x/sync | 261 | 0.0 | 21344 | 0.0 |
| github.com/lib/pq | 101 | 0.0 | 20232 | 0.0 |
| go.elastic.co/apm/module/apmfasthttp/v2 | 521 | 0.0 | 20080 | 0.0 |
| gitlab.datahub.ua.energy/tko/dhcore-go/ext/fiber/v3/apmfiber | 414 | 0.0 | 19352 | 0.0 |
| github.com/facebookgo/clock | 279 | 0.0 | 19248 | 0.0 |
| github.com/emersion/go-message | 164 | 0.0 | 18664 | 0.0 |
| github.com/samber/lo | 403 | 0.0 | 16064 | 0.0 |
| github.com/gofiber/utils/v2 | 290 | 0.0 | 7872 | 0.0 |
| github.com/golang-jwt/jwt/v5 | 67 | 0.0 | 7808 | 0.0 |
| github.com/h0rn3t/docx2pdf | 32 | 0.0 | 7088 | 0.0 |
| gorm.io/datatypes | 219 | 0.0 | 6648 | 0.0 |
| gitlab.datahub.ua.energy/tko/dhcore-go/redyska | 218 | 0.0 | 6008 | 0.0 |
| github.com/klauspost/compress | 2 | 0.0 | 5376 | 0.0 |
| github.com/pkg/errors | 57 | 0.0 | 4960 | 0.0 |
| github.com/gogo/protobuf | 4 | 0.0 | 4960 | 0.0 |
| gitlab.datahub.ua.energy/tko/dhcore-go/ext/http | 103 | 0.0 | 4616 | 0.0 |
| github.com/shopspring/decimal | 143 | 0.0 | 4528 | 0.0 |
| github.com/jinzhu/inflection | 1 | 0.0 | 4096 | 0.0 |
| gitlab.datahub.ua.energy/tko/dhcore-go/ext/fiber/v3/store/redyska | 154 | 0.0 | 3696 | 0.0 |
| go.elastic.co/apm/module/apmsql/v2 | 99 | 0.0 | 3624 | 0.0 |
| github.com/jackc/puddle/v2 | 45 | 0.0 | 2584 | 0.0 |
| github.com/grpc-ecosystem/grpc-gateway/v2 | 17 | 0.0 | 2144 | 0.0 |
| gitlab.datahub.ua.energy/tko/dhcore-go/try | 58 | 0.0 | 2048 | 0.0 |
| github.com/avast/retry-go/v4 | 30 | 0.0 | 1600 | 0.0 |
| github.com/joho/godotenv | 22 | 0.0 | 1560 | 0.0 |
| github.com/shakinm/xls!reader | 56 | 0.0 | 1264 | 0.0 |
| github.com/armon/go-radix | 22 | 0.0 | 1016 | 0.0 |
| gitlab.datahub.ua.energy/tko/dhcore-go/env | 50 | 0.0 | 800 | 0.0 |
| google.golang.org/genproto/googleapis/api | 1 | 0.0 | 768 | 0.0 |
| go.elastic.co/apm/module/apmgormv2/v2 | 12 | 0.0 | 448 | 0.0 |
| gitlab.datahub.ua.energy/tko/dhcore-go/ext/gorm/apmdriver | 10 | 0.0 | 352 | 0.0 |
| github.com/bsm/redislock | 9 | 0.0 | 240 | 0.0 |
| gitlab.datahub.ua.energy/tko/dhcore-go/ext/gorm | 7 | 0.0 | 224 | 0.0 |
| github.com/grpc-ecosystem/go-grpc-middleware/v2 | 6 | 0.0 | 192 | 0.0 |
| github.com/guregu/null | 11 | 0.0 | 176 | 0.0 |
| go.opentelemetry.io/otel/trace | 5 | 0.0 | 176 | 0.0 |
| github.com/r3labs/diff/v3 | 3 | 0.0 | 160 | 0.0 |
| github.com/pquerna/otp | 5 | 0.0 | 96 | 0.0 |
| github.com/gabriel-vasile/mimetype | 1 | 0.0 | 96 | 0.0 |
| github.com/mattn/go-colorable | 3 | 0.0 | 48 | 0.0 |
| github.com/guregu/null/v5 | 2 | 0.0 | 32 | 0.0 |
| разом | 6868063 | 100.0 | 2225338912 | 100.0 |

У `ms-faq` 23 модулі, 22 513 об'єктів і 44 337 552 байти. `go.elastic.co/apm/v2` тримає 34 972 800 байтів, 78.9% рядка. `поза кешем` — 864 байти.

| Модуль | Об'єкти | % об'єктів | Байти | % байтів |
| --- | ---: | ---: | ---: | ---: |
| go.elastic.co/apm/v2 | 2179 | 9.7 | 34972800 | 78.9 |
| github.com/elastic/go-sysinfo | 507 | 2.3 | 7158848 | 16.1 |
| github.com/gofiber/fiber/v3 | 10380 | 46.1 | 1382888 | 3.1 |
| github.com/valyala/fasthttp | 2560 | 11.4 | 372384 | 0.8 |
| gorm.io/gorm | 4313 | 19.2 | 207224 | 0.5 |
| gitlab.datahub.ua.energy/tko/dhcore-go/ext/fiber/v3/xuser | 694 | 3.1 | 84768 | 0.2 |
| go.elastic.co/fastjson | 160 | 0.7 | 72480 | 0.2 |
| howett.net/plist | 882 | 3.9 | 37728 | 0.1 |
| github.com/valyala/bytebufferpool | 144 | 0.6 | 9248 | 0.0 |
| gitlab.datahub.ua.energy/tko/dhcore-go/error-provider | 152 | 0.7 | 8464 | 0.0 |
| gorm.io/driver/postgres | 88 | 0.4 | 7216 | 0.0 |
| github.com/gofiber/utils/v2 | 131 | 0.6 | 6160 | 0.0 |
| github.com/google/uuid | 180 | 0.8 | 5824 | 0.0 |
| golang.org/x/text | 1 | 0.0 | 3456 | 0.0 |
| github.com/klauspost/compress | 1 | 0.0 | 2688 | 0.0 |
| go.elastic.co/apm/module/apmhttp/v2 | 30 | 0.1 | 1920 | 0.0 |
| поза | 18 | 0.1 | 864 | 0.0 |
| go.elastic.co/apm/module/apmgormv2/v2 | 22 | 0.1 | 792 | 0.0 |
| go.elastic.co/apm/module/apmfasthttp/v2 | 24 | 0.1 | 688 | 0.0 |
| go.elastic.co/apm/module/apmsql/v2 | 22 | 0.1 | 616 | 0.0 |
| github.com/jackc/pgx/v5 | 12 | 0.1 | 288 | 0.0 |
| gitlab.datahub.ua.energy/tko/dhcore-go/env | 8 | 0.0 | 128 | 0.0 |
| github.com/lib/pq | 5 | 0.0 | 80 | 0.0 |
| разом | 22513 | 100.0 | 44337552 | 100.0 |
