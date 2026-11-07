//go:build owngo && owngostat

// Програма прийняття P-NEW-08.1 і P-NEW-08.2: облік алокатора під тегом
// owngostat і відкладене повернення сторінок. Батьківський процес запускає
// себе дочірнім у трьох режимах і звіряє рядки owngo alloc, які дитина
// друкує через reportAlloc рантайму і ReportExit на виході.
//
// ALLOCSTAT=load — між знімками A і B дитина звертається до алокатора:
// 10⁵ малих блоків, 100 великих по 1 МіБ і 1000 малих блоків, які
// звільняє інший потік. Різниця B−A точна. Далі — простій і один великий
// блок, після чого пул віддає ОС звільнені сторінки: нижні межі різниці
// рядків виходу і A.
//
// ALLOCSTAT=cycles:N — N циклів «зайняти 64 МіБ, звільнити»: madvise
// між 10 і 100 циклами не росте.
//
// ALLOCSTAT=rss — 64 МіБ із записом кожної сторінки, звільнення і
// простій: RSS падає.
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"
)

//go:linkname allocRoot github.com/xgo-dev/llgo/runtime/internal/runtime.AllocRoot
func allocRoot(size uintptr) unsafe.Pointer

//go:linkname freeRoot github.com/xgo-dev/llgo/runtime/internal/runtime.FreeRoot
func freeRoot(ptr unsafe.Pointer)

// reportAlloc друкує рядки owngo alloc зараз, а не лише на виході.
//
//go:linkname reportAlloc github.com/xgo-dev/llgo/runtime/internal/runtime.reportAlloc
func reportAlloc()

// usleep замість time.Sleep: Sleep виділяє канал і таймер.
//
//go:linkname usleep C.usleep
func usleep(us uint32) int32

const (
	smalls  = 100_000
	larges  = 100
	remotes = 1000
	// Пул алокатора тримає до 64 сторінок по 64 КіБ зайнятими (P8.3).
	pageSize = 64 << 10
	poolKeep = 64
	// Великий блок 1 МіБ займає 16 сторінок.
	largePages = (1 << 20) / pageSize
	// cycleBlocks блоків по 1 МіБ — 64 МіБ на цикл.
	cycleBlocks = 64
	// idle — простій у мікросекундах, більший за затримку повернення
	// сторінок алокатора (P-NEW-08.2).
	idle = 2 * time.Second / time.Microsecond
)

// want — точна різниця знімків B і A.
var want = map[string]uint64{
	"small":       smalls + remotes,
	"large":       larges,
	"free_local":  smalls,
	"free_remote": remotes,
	"free_large":  larges,
}

var slots [smalls]unsafe.Pointer

// load. Горутина стартує до знімка A і чекає після знімка B: потік,
// який під неї створюється і завершується, виділяє і звільняє блоки
// залежно від планування, тож його життя лишається поза вікном A–B.
func load() {
	var start, freed, release atomic.Bool
	go func() {
		for !start.Load() {
			usleep(100)
		}
		for i := range remotes {
			freeRoot(slots[i])
		}
		freed.Store(true)
		for !release.Load() {
			usleep(100)
		}
	}()
	reportAlloc() // A
	for i := range smalls {
		slots[i] = allocRoot(64)
	}
	for i := range smalls {
		freeRoot(slots[i])
	}
	for i := range larges {
		slots[i] = allocRoot(1 << 20)
	}
	for i := range larges {
		freeRoot(slots[i])
	}
	for i := range remotes {
		slots[i] = allocRoot(32)
	}
	start.Store(true)
	for !freed.Load() {
		usleep(100)
	}
	reportAlloc() // B
	release.Store(true)
	idleTrigger()
}

// idleTrigger простоює і одним великим блоком дає алокатору повернути ОС
// сторінки, що пролежали в пулі довше за затримку.
func idleTrigger() {
	usleep(uint32(idle))
	freeRoot(allocRoot(1 << 20))
}

func cycles(n int) {
	for range n {
		for i := range cycleBlocks {
			slots[i] = allocRoot(1 << 20)
		}
		for i := range cycleBlocks {
			freeRoot(slots[i])
		}
	}
}

// resident — RSS процесу в байтах з /proc/self/statm.
func resident() uint64 {
	b, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		panic(err)
	}
	pages, err := strconv.ParseUint(strings.Fields(string(b))[1], 10, 64)
	if err != nil {
		panic(err)
	}
	return pages * uint64(os.Getpagesize())
}

func rss() {
	for i := range cycleBlocks {
		p := allocRoot(1 << 20)
		for off := 0; off < 1<<20; off += 4096 {
			*(*byte)(unsafe.Add(p, off)) = 1
		}
		slots[i] = p
	}
	peak := resident()
	for i := range cycleBlocks {
		freeRoot(slots[i])
	}
	idleTrigger()
	println("allocstat-rss", peak, resident())
}

func main() {
	if s := os.Getenv("ALLOCSTAT"); s != "" {
		switch {
		case s == "load":
			load()
		case s == "rss":
			rss()
		case strings.HasPrefix(s, "cycles:"):
			n, err := strconv.Atoi(strings.TrimPrefix(s, "cycles:"))
			if err != nil {
				panic(err)
			}
			cycles(n)
		default:
			panic("bad ALLOCSTAT " + s)
		}
		return
	}
	for _, check := range []func() error{checkLoad, checkCycles, checkRSS} {
		if err := check(); err != nil {
			fmt.Fprintf(os.Stderr, "allocstat: %v\n", err)
			os.Exit(1)
		}
	}
	fmt.Println("allocstat-ok")
}

// run запускає дитину і повертає кожен набір рядків owngo alloc у порядку
// друку і весь вивід.
func run(mode string) ([]map[string]uint64, string, error) {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "ALLOCSTAT="+mode)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return nil, "", fmt.Errorf("child %s: %v\n%s", mode, err, out.String())
	}
	var sets []map[string]uint64
	sc := bufio.NewScanner(strings.NewReader(out.String()))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) != 4 || f[0] != "owngo" || f[1] != "alloc" {
			continue
		}
		v, err := strconv.ParseUint(f[3], 10, 64)
		if err != nil {
			return nil, "", err
		}
		if len(sets) == 0 {
			sets = append(sets, map[string]uint64{})
		}
		if _, seen := sets[len(sets)-1][f[2]]; seen {
			sets = append(sets, map[string]uint64{})
		}
		sets[len(sets)-1][f[2]] = v
	}
	if len(sets) == 0 {
		return nil, "", fmt.Errorf("child %s: no owngo alloc lines\n%s", mode, out.String())
	}
	return sets, out.String(), nil
}

// delta — різниця рядка name між наборами from і to.
func delta(from, to map[string]uint64, name string) (uint64, error) {
	a, ok1 := from[name]
	b, ok2 := to[name]
	if !ok1 || !ok2 {
		return 0, fmt.Errorf("no owngo alloc %s line", name)
	}
	if b < a {
		return 0, fmt.Errorf("owngo alloc %s went down: %d to %d", name, a, b)
	}
	return b - a, nil
}

func checkLoad() error {
	sets, out, err := run("load")
	if err != nil {
		return err
	}
	if len(sets) != 3 {
		return fmt.Errorf("child load: %d sets of owngo alloc lines, want 3 (A, B, exit)\n%s", len(sets), out)
	}
	a, b, exit := sets[0], sets[1], sets[2]
	for name, w := range want {
		d, err := delta(a, b, name)
		if err != nil {
			return err
		}
		if d != w {
			return fmt.Errorf("owngo alloc %s B-A = %d, want %d", name, d, w)
		}
	}
	// Звільнені великі блоки повертають у пул щонайменше larges·16 сторінок,
	// і після простою понад poolKeep з них ідуть ОС через madvise.
	lower := map[string]uint64{
		"page_give":     larges * largePages,
		"madvise":       1,
		"madvise_bytes": (larges*largePages - poolKeep) * pageSize,
		"lock":          2 * larges,
	}
	for name, w := range lower {
		d, err := delta(a, exit, name)
		if err != nil {
			return err
		}
		if d < w {
			return fmt.Errorf("owngo alloc %s exit-A = %d, want at least %d", name, d, w)
		}
	}
	for _, name := range []string{"page_new", "page_reuse"} {
		if _, err := delta(a, exit, name); err != nil {
			return err
		}
	}
	return nil
}

// checkCycles: пул тримає звільнені сторінки, доки вони потрібні, тож
// 90 додаткових циклів не дають madvise і на один цикл (64 МіБ).
func checkCycles() error {
	few, _, err := run("cycles:10")
	if err != nil {
		return err
	}
	many, _, err := run("cycles:100")
	if err != nil {
		return err
	}
	a, b := few[len(few)-1]["madvise_bytes"], many[len(many)-1]["madvise_bytes"]
	if b >= a+cycleBlocks<<20 {
		return fmt.Errorf("owngo alloc madvise_bytes: %d after 100 cycles, %d after 10; want growth under %d", b, a, cycleBlocks<<20)
	}
	return nil
}

// checkRSS: після простою пул віддає ОС усе понад poolKeep сторінок, тож
// RSS падає щонайменше на 64 МіБ без пулу (4 МіБ) і запасу 12 МіБ.
func checkRSS() error {
	_, out, err := run("rss")
	if err != nil {
		return err
	}
	var peak, after uint64
	for line := range strings.Lines(out) {
		if _, rest, ok := strings.Cut(line, "allocstat-rss "); ok {
			if _, err := fmt.Sscan(rest, &peak, &after); err != nil {
				return fmt.Errorf("bad allocstat-rss line %q: %v", line, err)
			}
		}
	}
	if peak == 0 {
		return fmt.Errorf("no allocstat-rss line\n%s", out)
	}
	const drop = (cycleBlocks - poolKeep*pageSize>>20 - 12) << 20
	if after+drop > peak {
		return fmt.Errorf("RSS %d after idle, %d at peak; want a drop of at least %d", after, peak, drop)
	}
	return nil
}
