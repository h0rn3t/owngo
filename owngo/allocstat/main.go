//go:build owngo && owngostat

// Програма прийняття P-NEW-08.1: облік алокатора під тегом owngostat.
// Батьківський процес двічі запускає себе дочірнім: з ALLOCSTAT=0 і
// ALLOCSTAT=1. Обидві дитини роблять ту саму роботу Go, але лише друга
// звертається до алокатора: 10⁵ малих блоків, 100 великих по 1 МіБ і
// 1000 малих блоків, які звільняє інший потік. На виході ReportExit
// друкує рядки owngo alloc; батько звіряє різницю двох дітей.
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"unsafe"
)

//go:linkname allocRoot github.com/xgo-dev/llgo/runtime/internal/runtime.AllocRoot
func allocRoot(size uintptr) unsafe.Pointer

//go:linkname freeRoot github.com/xgo-dev/llgo/runtime/internal/runtime.FreeRoot
func freeRoot(ptr unsafe.Pointer)

const (
	smalls  = 100_000
	larges  = 100
	remotes = 1000
	// Пул алокатора тримає до 64 сторінок по 64 КіБ зайнятими (P8.3).
	pageSize = 64 << 10
	poolKeep = 64
	// Великий блок 1 МіБ займає 16 сторінок.
	largePages = (1 << 20) / pageSize
)

// want — точна різниця рядків owngo alloc між дітьми.
var want = map[string]uint64{
	"small":       smalls + remotes,
	"large":       larges,
	"free_local":  smalls,
	"free_remote": remotes,
	"free_large":  larges,
}

// slots тримає блоки обох дітей: масив виділяється однаково в кожній.
var slots [smalls]unsafe.Pointer

func child(on bool) {
	n, l, r := 0, 0, 0
	if on {
		n, l, r = smalls, larges, remotes
	}
	for i := range n {
		slots[i] = allocRoot(64)
	}
	for i := range n {
		freeRoot(slots[i])
	}
	for i := range l {
		slots[i] = allocRoot(1 << 20)
	}
	for i := range l {
		freeRoot(slots[i])
	}
	for i := range r {
		slots[i] = allocRoot(32)
	}
	done := make(chan struct{})
	go func() {
		for i := range r {
			freeRoot(slots[i])
		}
		close(done)
	}()
	<-done
	println("allocstat-child", n, l, r)
}

func main() {
	if s := os.Getenv("ALLOCSTAT"); s != "" {
		child(s == "1")
		return
	}
	off, err := run("0")
	if err != nil {
		fail("%v", err)
	}
	on, err := run("1")
	if err != nil {
		fail("%v", err)
	}
	if err := check(off, on); err != nil {
		fail("%v", err)
	}
	fmt.Println("allocstat-ok")
}

func run(mode string) (map[string]uint64, error) {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "ALLOCSTAT="+mode)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("child %s: %v\n%s", mode, err, out.String())
	}
	rows := map[string]uint64{}
	sc := bufio.NewScanner(strings.NewReader(out.String()))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) != 4 || f[0] != "owngo" || f[1] != "alloc" {
			continue
		}
		v, err := strconv.ParseUint(f[3], 10, 64)
		if err != nil {
			return nil, err
		}
		rows[f[2]] = v
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("child %s: no owngo alloc lines\n%s", mode, out.String())
	}
	return rows, nil
}

func check(off, on map[string]uint64) error {
	delta := func(name string) (uint64, error) {
		a, ok1 := off[name]
		b, ok2 := on[name]
		if !ok1 || !ok2 {
			return 0, fmt.Errorf("no owngo alloc %s line", name)
		}
		if b < a {
			return 0, fmt.Errorf("owngo alloc %s: %d with load < %d without", name, b, a)
		}
		return b - a, nil
	}
	for name, w := range want {
		d, err := delta(name)
		if err != nil {
			return err
		}
		if d != w {
			return fmt.Errorf("owngo alloc %s delta = %d, want %d", name, d, w)
		}
	}
	// Звільнені великі блоки повертають у пул щонайменше larges·16 сторінок,
	// і понад poolKeep з них ідуть ОС через madvise.
	lower := map[string]uint64{
		"page_give":     larges * largePages,
		"madvise":       1,
		"madvise_bytes": (larges*largePages - poolKeep) * pageSize,
		"lock":          2 * larges,
	}
	for name, w := range lower {
		d, err := delta(name)
		if err != nil {
			return err
		}
		if d < w {
			return fmt.Errorf("owngo alloc %s delta = %d, want at least %d", name, d, w)
		}
	}
	// Нова сторінка береться за межею або з пулу; рядки є завжди.
	for _, name := range []string{"page_new", "page_reuse"} {
		if _, err := delta(name); err != nil {
			return err
		}
	}
	return nil
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "allocstat: "+format+"\n", args...)
	os.Exit(1)
}
