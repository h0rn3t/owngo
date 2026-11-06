package main

import (
	"path/filepath"
	"testing"
)

// TestGapRows — кожен вид алокації фікстури проти діагностики gc 1.27
// для того самого об'єкта, а не для рядка.
func TestGapRows(t *testing.T) {
	dir, err := filepath.Abs(filepath.Join("testdata", "prog"))
	if err != nil {
		t.Fatal(err)
	}
	at := func(line string) string { return "prog/main.go:" + line }
	rows := []siteRow{
		{Pos: at("30"), Category: "static", N: 1},
		{Pos: at("31"), Category: "static", N: 2},
		{Pos: at("32"), Category: "rc-shared", N: 3},
		{Pos: at("33"), Category: "static", N: 4},
		{Pos: at("34"), Category: "rc-local", N: 5},
		{Pos: at("35"), Category: "static", N: 6},
		{Pos: at("36"), Category: "rc-local", N: 7},
		{Pos: at("37"), Category: "static", N: 8},
		{Pos: at("38"), Category: "rc-shared", N: 9},
		{Pos: at("40"), Category: "rc-shared", N: 10},
		{Pos: at("41"), Category: "static", N: 11},
		{Pos: ":0", Category: "static", N: 12},
		{Pos: "runtime/internal/runtime/z_gc.go:7", Category: "unplaced", N: 13},
	}
	gcText, err := gcOutput(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := gapRows(dir, filepath.Join(dir, ".."), rows, gcText)
	if err != nil {
		t.Fatalf("gapRows() error = %v", err)
	}
	want := []struct{ verdict, kind string }{
		{"gap", "new"},
		{"gap", "complit"},
		{"heap", "complit"},
		{"gap", "args+var"}, // n захоплено замиканням, gc лишає n на стеку
		{"heap", "args+new"},
		{"gap", "append-args"},
		{"gap", "var"},
		{"gap", "closure"},
		{"heap", "var"},
		{"heap", "closure"},
		{"gap", "makeslice"},
		{"none", "nopos"},
		{"none", "-"},
	}
	if len(got) != len(want) {
		t.Fatalf("gapRows() = %d rows, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Verdict != w.verdict || got[i].Kind != w.kind {
			t.Errorf("gapRows(%s) = %s %s, want %s %s", rows[i].Pos, got[i].Verdict, got[i].Kind, w.verdict, w.kind)
		}
	}
}

func TestReadSites(t *testing.T) {
	const text = `owngo freed 10
owngo ops alloc 40
owngo site /a/b.go:3 static - alloc 30
owngo site /a/b.go:3 static - free_accounted 30
owngo site /a/b.go:5 cyclic return alloc_counted 6
owngo site /a/b.go:5 cyclic return note_alloc 6
owngo site /a/c.go:9 rc-local call alloc 4
`
	rows, total, err := readSites([]byte(text))
	if err != nil {
		t.Fatalf("readSites() error = %v", err)
	}
	if total != 40 {
		t.Errorf("readSites() ops alloc = %d, want 40", total)
	}
	want := []siteRow{
		{Pos: "/a/b.go:3", Category: "static", Reason: "-", N: 30},
		{Pos: "/a/b.go:5", Category: "cyclic", Reason: "return", N: 6},
		{Pos: "/a/c.go:9", Category: "rc-local", Reason: "call", N: 4},
	}
	if len(rows) != len(want) {
		t.Fatalf("readSites() = %+v, want %+v", rows, want)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("readSites() row %d = %+v, want %+v", i, rows[i], want[i])
		}
	}
}
