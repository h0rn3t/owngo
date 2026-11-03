//go:build owngo

// Фікстура P11.3: string від порожнього зрізу — рядок з data == nil, і його
// звільнення не мусить падати у власному алокаторі owngo
// (GOROOT/test bug102.go, issue13162.go, issue23536.go, issue23814.go,
// named.go). Під ASan free(nil) нічого не робить, тож падіння видно лише без
// -asan: llgo run -tags owngo має завершитись кодом 0.
package main

import "os"

func conv(b []byte) int {
	s := string(b)
	return len(s)
}

func main() {
	var b [0]byte
	if conv(b[0:]) != 0 || conv(make([]byte, len(os.Args)-1)) != 0 {
		os.Exit(1)
	}
}
