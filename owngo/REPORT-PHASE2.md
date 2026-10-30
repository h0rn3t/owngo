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
