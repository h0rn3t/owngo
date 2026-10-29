package census

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestStackGaps(t *testing.T) {
	sites := []Site{
		{Category: "static", Pos: "/src/static.go:5"},
		{Category: "rc-local", Reason: "return", Pos: "/src/ret.go:4"},
		{Category: "stack", Pos: "/src/stack.go:8"},
		{Category: "static", Pos: "/src/chan.go:4"},
		{Category: "implicit", Reason: "append", Pos: "/src/app.go:6"},
		{Category: "static", Pos: "/src/mix.go:3"},
		{Category: "static", Pos: "/mod/internal/app/a.go:9"},
	}
	diags := []GCDiag{
		{File: "static.go", Line: 5, Stack: true},
		{File: "ret.go", Line: 4, Heap: true},
		{File: "stack.go", Line: 8, Stack: true},
		{File: "app.go", Line: 6, Stack: true},
		{File: "mix.go", Line: 3, Stack: true},
		{File: "mix.go", Line: 3, Heap: true},
		{File: "internal/app/a.go", Line: 9, Stack: true},
	}
	got := StackGaps(sites, diags)
	want := []string{"/src/static.go:5", "/src/app.go:6", "/mod/internal/app/a.go:9"}
	var pos []string
	for _, site := range got {
		pos = append(pos, site.Pos)
	}
	if !slices.Equal(pos, want) {
		t.Errorf("StackGaps() = %v, want %v", pos, want)
	}
}

func TestStackGapsEmpty(t *testing.T) {
	got := StackGaps(nil, nil)
	if got == nil || len(got) != 0 {
		t.Errorf("StackGaps(nil, nil) = %#v, want empty slice", got)
	}
}

func TestParseGCText(t *testing.T) {
	const text = `# corpus
./static.go:5:10: new(int) does not escape
./rclocal.go:4:10: new(int) escapes to heap
./rclocal.go:69:6: moved to heap: a
./implicit.go:6:12: append does not escape
./implicit.go:20:22: b does not escape
./rcshared.go:36:11: ch does not escape
./rclocal.go:53:8: b.N does not escape
./stack.go:7:6: can inline stackArray
./static.go:51:2: n does not escape
`
	got, err := ParseGCText(strings.NewReader(text))
	if err != nil {
		t.Fatalf("ParseGCText() error = %v", err)
	}
	want := []GCDiag{
		{File: "./static.go", Line: 5, Stack: true},
		{File: "./rclocal.go", Line: 4, Heap: true},
		{File: "./rclocal.go", Line: 69, Heap: true},
		{File: "./implicit.go", Line: 6, Stack: true},
		{File: "./rclocal.go", Line: 53, Stack: true},
	}
	if !slices.Equal(got, want) {
		t.Errorf("ParseGCText() = %+v, want %+v", got, want)
	}
}

func TestParseGCJSON(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "corpus")
	if err := os.Mkdir(pkg, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	body := `{"version":0,"package":"corpus","file":"/src/static.go"}
{"range":{"start":{"line":5,"character":10},"end":{"line":5,"character":10}},"severity":3,"code":"escape","source":"go compiler","message":"new(int) escapes to heap"}
{"range":{"start":{"line":7,"character":6},"end":{"line":7,"character":6}},"severity":3,"code":"canInlineFunction","source":"go compiler","message":"cost: 1"}
{"range":{"start":{"line":9,"character":2},"end":{"line":9,"character":2}},"severity":3,"code":"escapes","source":"go compiler","message":"a escapes to heap"}
`
	if err := os.WriteFile(filepath.Join(pkg, "static.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	got, err := ParseGCJSON(dir)
	if err != nil {
		t.Fatalf("ParseGCJSON() error = %v", err)
	}
	want := []GCDiag{
		{File: "/src/static.go", Line: 5, Heap: true},
		{File: "/src/static.go", Line: 9, Heap: true},
	}
	if !slices.Equal(got, want) {
		t.Errorf("ParseGCJSON() = %+v, want %+v", got, want)
	}
}

// TestCorpusGCGap — місця корпусу, де go/ssa кладе в купу, а gc лишає на стеку.
// Список зібрано вручну з діагностик gc 1.27.3 для testdata/src/corpus.
func TestCorpusGCGap(t *testing.T) {
	testdata := analysistest.TestData()
	results := analysistest.Run(t, testdata, Analyzer, "corpus")
	if len(results) != 1 || results[0].Err != nil {
		var err error
		if len(results) == 1 {
			err = results[0].Err
		}
		t.Fatalf("corpus results = %d, err = %v", len(results), err)
	}
	sites, ok := results[0].Result.([]Site)
	if !ok {
		t.Fatalf("Result = %T, want []Site", results[0].Result)
	}
	diags, err := RunGC(filepath.Join(testdata, "src", "corpus"), []string{"."}, []string{
		"GO111MODULE=off",
		"GOPATH=" + testdata,
	})
	if err != nil {
		t.Fatalf("RunGC(corpus) error = %v", err)
	}
	got := StackGaps(sites, diags)
	var pos []string
	for _, site := range got {
		pos = append(pos, filepath.Base(site.Pos))
	}
	slices.Sort(pos)
	want := []string{
		"cyclic.go:26",
		"cyclic.go:34",
		"cyclic.go:41",
		"cyclic.go:60",
		"implicit.go:13",
		"implicit.go:15",
		"implicit.go:15",
		"implicit.go:29",
		"implicit.go:5",
		"implicit.go:6",
		"implicit.go:6",
		"pinned.go:12",
		"pinned.go:18",
		"pinned.go:24",
		"pinned.go:30",
		"pinned.go:42",
		"pinned.go:6",
		"rclocal.go:25",
		"rclocal.go:52",
		"rclocal.go:53",
		"static.go:12",
		"static.go:18",
		"static.go:26",
		"static.go:38",
		"static.go:45",
		"static.go:5",
	}
	if !slices.Equal(pos, want) {
		t.Errorf("StackGaps(corpus) = %v, want %v", pos, want)
	}
	for _, keep := range []string{"static.go:32", "rclocal.go:4", "cyclic.go:7"} {
		if slices.Contains(pos, keep) {
			t.Errorf("StackGaps(corpus) contains %s", keep)
		}
	}
}
