//go:build owngo && owngomn

// Програма прийняття P-NEW-09.7: 10 000 TCP-з'єднань через loopback. Серверна
// горутина кожного з'єднання чекає на читанні; коли чекають усі, рахуємо
// потоки процесу, потім кожне з'єднання отримує рядок і відповідає. Дедлайн
// читання на з'єднанні, яке мовчить, має спрацювати. Код 0 лише якщо всі
// відповіли, дедлайн спрацював, а потоків не більше за GOMAXPROCS + 4.
package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"
)

const n, procs = 10_000, 4

func threads() int {
	ents, err := os.ReadDir("/proc/self/task")
	if err != nil {
		return -1 // немає /proc (macOS): кількість потоків не перевіряється
	}
	return len(ents)
}

// raiseNofile піднімає м'який ліміт дескрипторів до жорсткого: на кожне
// з'єднання їх два, а на macOS типовий м'який ліміт — 256.
func raiseNofile() {
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		return
	}
	lim.Cur = lim.Max
	_ = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &lim)
}

func fail(code int, format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(code)
}

func main() {
	runtime.GOMAXPROCS(procs)
	raiseNofile()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fail(2, "listen: %v", err)
	}
	var waiting, served atomic.Int32
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				var req [11]byte
				waiting.Add(1)
				if _, err := io.ReadFull(conn, req[:]); err != nil {
					fail(5, "server read: %v", err)
				}
				var i int
				if _, err := fmt.Sscanf(string(req[:]), "ping %05d\n", &i); err != nil {
					fail(5, "server parse %q: %v", req[:], err)
				}
				if _, err := fmt.Fprintf(conn, "pong %05d\n", i); err != nil {
					fail(5, "server write: %v", err)
				}
				served.Add(1)
			}()
		}
	}()

	clients := make([]net.Conn, n)
	for i := range clients {
		c, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			fail(2, "dial %d: %v", i, err)
		}
		clients[i] = c
	}
	for start := time.Now(); waiting.Load() < n; time.Sleep(time.Millisecond) {
		if time.Since(start) > 30*time.Second {
			fail(2, "only %d of %d server goroutines reached the read", waiting.Load(), n)
		}
	}
	time.Sleep(100 * time.Millisecond) // дати останнім горутинам заблокуватися в читанні
	th := threads()

	// Дедлайн: сервер цьому з'єднанню ще нічого не надіслав.
	quiet := clients[0]
	t0 := time.Now()
	quiet.SetReadDeadline(t0.Add(50 * time.Millisecond))
	var b [1]byte
	_, err = quiet.Read(b[:])
	waited := time.Since(t0)
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		fail(3, "read with deadline: err %v, want deadline exceeded", err)
	}
	if waited < 50*time.Millisecond || waited > 5*time.Second {
		fail(3, "read deadline fired after %v, want about 50ms", waited)
	}
	quiet.SetReadDeadline(time.Time{})

	for i, c := range clients {
		if _, err := fmt.Fprintf(c, "ping %05d\n", i); err != nil {
			fail(2, "client write %d: %v", i, err)
		}
	}
	for i, c := range clients {
		var resp [11]byte
		if _, err := io.ReadFull(c, resp[:]); err != nil {
			fail(2, "client read %d: %v", i, err)
		}
		if want := fmt.Sprintf("pong %05d\n", i); string(resp[:]) != want {
			fail(2, "client %d got %q, want %q", i, resp[:], want)
		}
		c.Close()
	}
	ln.Close()

	fmt.Printf("mn-idleconns conns %d served %d threads %d deadline ok\n", n, served.Load(), th)
	if th > procs+4 {
		fail(4, "threads %d, want at most %d", th, procs+4)
	}
}
