//go:build owngo && owngomn

// Програма прийняття P-NEW-09.5: вихід за резерв стека G — fatal error,
// а не мовчазний SIGSEGV. Батьківський процес запускає себе з аргументом
// child; дочірній рекурсивно заглиблюється на стеку G у 256 КіБ. Код 0 лише
// якщо дочірній завершився з кодом 2 і написав «fatal error: stack overflow»
// та «goroutine N».
package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"regexp"
	_ "unsafe"
)

//go:linkname mnRun github.com/xgo-dev/llgo/runtime/internal/runtime.MNRunForTesting
func mnRun(stack uintptr, f func()) bool

var sink [64]byte

//go:noinline
func deep(n int) int {
	var buf [256]byte
	buf[n%len(buf)] = byte(n)
	sink[n%len(sink)] = buf[(n*7)%len(buf)]
	return deep(n+1) + int(buf[0])
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "child" {
		mnRun(256<<10, func() { deep(0) })
		println("child: recursion returned")
		os.Exit(0)
	}
	cmd := exec.Command(os.Args[0], "child")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 {
		println("child exit:", err, "want code 2")
		os.Stderr.Write(stderr.Bytes())
		os.Exit(2)
	}
	if !bytes.Contains(stderr.Bytes(), []byte("fatal error: stack overflow")) ||
		!regexp.MustCompile(`goroutine \d+`).Match(stderr.Bytes()) {
		println("child stderr lacks the stack overflow report:")
		os.Stderr.Write(stderr.Bytes())
		os.Exit(3)
	}
	if bytes.Contains(stderr.Bytes(), []byte("AddressSanitizer")) {
		println("child: ASan report")
		os.Stderr.Write(stderr.Bytes())
		os.Exit(4)
	}
}
