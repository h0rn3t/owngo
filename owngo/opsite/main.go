//go:build owngo && owngostat

// Програма прийняття P-NEW-02.1: таблиця місць під тегом owngostat.
// Батьківський процес запускає себе дочірнім з OPSITE_N=n: дитина n разів
// проходить місця static, rc-local, cyclic і спільне через go, а на виході
// ReportExit друкує рядки owngo site і owngo ops. Батько звіряє рядки цього
// файлу з want і суму рядків за операцією з owngo ops; код 0 — збіг.
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type Tree struct{ Left, Right *Tree }

func NewTree(depth int) *Tree {
	if depth > 0 {
		return &Tree{Left: NewTree(depth - 1), Right: NewTree(depth - 1)}
	}
	return &Tree{}
}

func local(depth int) int {
	a := NewTree(depth)
	if a.Left == nil {
		return 1
	}
	return 2
}

func staticNew() int {
	p := new(int)
	*p = 1
	return *p
}

func closureUse() int {
	p := new(int)
	*p = 1
	f := func() int { return *p }
	return f() + *p
}

type Counter struct{ n int }

func (c *Counter) Add(d int) { c.n += d }

func capture(done chan struct{}) int {
	var c Counter
	go func() {
		c.Add(1)
		done <- struct{}{}
	}()
	<-done
	c.Add(2)
	return c.n
}

func child(n int) {
	done := make(chan struct{})
	s := 0
	for range n {
		s += staticNew() + closureUse() + local(1) + capture(done) + cyclicKept(1)
	}
	println("opsite-child", s)
}

// want — рядки цього файлу: «рядок категорія причина операція» → разів за
// ітерацію дитини. NewTree(1) дає три вузли: у local — у регіоні клону
// NewTree$region (P-NEW-05.4), у cyclicKept — AllocCounted під DecCyclic.
// У closureUse два купові Alloc на одному рядку — слот змінної і new(int).
var want = map[string]uint64{
	"24 cyclic return alloc_counted":    1,
	"26 cyclic return alloc_counted":    2,
	"24 cyclic return alloc_region":     1,
	"26 cyclic return alloc_region":     2,
	"30 cyclic - region_create":         1,
	"30 cyclic - free_region":           1,
	"189 cyclic - dec_cyclic":           1,
	"38 static - alloc":                 1,
	"38 static - note_alloc":            1,
	"38 static - free_accounted":        1,
	"44 rc-local closure alloc_counted": 2,
	"44 rc-local closure dec_accounted": 2,
	"54 rc-shared go inc_shared":        1,
	"54 rc-shared go dec_shared":        2,
}

func main() {
	if s := os.Getenv("OPSITE_N"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			panic(err)
		}
		child(n)
		return
	}
	n := 1000
	if len(os.Args) > 1 {
		var err error
		if n, err = strconv.Atoi(os.Args[1]); err != nil {
			fail("bad n: %v", err)
		}
	}
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "OPSITE_N="+strconv.Itoa(n))
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		fail("child: %v\n%s", err, out.String())
	}
	if err := check(out.String(), uint64(n)); err != nil {
		fail("%v\n%s", err, out.String())
	}
	fmt.Println("opsite-ok", n)
}

func check(report string, n uint64) error {
	ops := map[string]uint64{}
	sums := map[string]uint64{}
	mine := map[string]uint64{}
	sc := bufio.NewScanner(strings.NewReader(report))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		switch {
		case len(f) == 4 && f[0] == "owngo" && f[1] == "ops":
			v, err := strconv.ParseUint(f[3], 10, 64)
			if err != nil {
				return err
			}
			ops[f[2]] = v
		case len(f) == 7 && f[0] == "owngo" && f[1] == "site":
			v, err := strconv.ParseUint(f[6], 10, 64)
			if err != nil {
				return err
			}
			cat, op := f[3], f[5]
			if op == "note_alloc" {
				op += "_" + cat
			}
			sums[op] += v
			file, line, _ := strings.Cut(f[2], ":")
			if strings.HasSuffix(file, "/opsite/main.go") {
				mine[line+" "+strings.Join(f[3:6], " ")] += v
			}
		}
	}
	if len(ops) == 0 {
		return fmt.Errorf("no owngo ops lines")
	}
	for row, k := range want {
		if got := mine[row]; got != k*n {
			return fmt.Errorf("site %q = %d, want %d", row, got, k*n)
		}
	}
	// alloc рантайму рахує кожен AllocU, зокрема всередині MakeMap,
	// росту зрізу і самого AllocCounted; рядок місця — одне виконання
	// інструкції. Тож для alloc сума місць — нижня межа.
	if a := sums["alloc"] + sums["alloc_counted"]; a > ops["alloc"] {
		return fmt.Errorf("site alloc+alloc_counted = %d > owngo ops alloc %d", a, ops["alloc"])
	}
	for op, v := range ops {
		switch op {
		case "alloc", "alloc_root", "free_root", "cyclic_visit":
			continue // рантайм ставить їх сам, без місця
		}
		if sums[op] != v {
			return fmt.Errorf("site sum %s = %d, owngo ops %s = %d", op, sums[op], op, v)
		}
	}
	return nil
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "opsite: "+format+"\n", args...)
	os.Exit(1)
}

// cyclicKept — дерево, у яке пишуть після побудови, не стає регіоном
// (P-NEW-05.2) і лишається на DecCyclic. Лист a.Left і так має Left == nil,
// тож запис нічого не відриває.
func cyclicKept(depth int) int {
	a := NewTree(depth)
	a.Left.Left = nil
	if a.Right == nil {
		return 1
	}
	return 2
}
