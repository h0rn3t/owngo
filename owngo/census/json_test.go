package census

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	json "encoding/json/v2"

	"golang.org/x/tools/go/analysis/analysistest"
)

// site — контракт одного об'єкта в JSON census -json.
// Порожня reason лишається ключем: stack, static і cyclic без втечі.
type site struct {
	Category string `json:"category"`
	Reason   string `json:"reason"`
	Pos      string `json:"pos"`
	Type     string `json:"type"`
}

// TestSitesJSON перевіряє результат аналізатора: масив місць із чотирма
// полями, той самий набір категорій, що й // want, і тип значення SSA
// на контрольних місцях.
func TestSitesJSON(t *testing.T) {
	results := analysistest.Run(t, analysistest.TestData(), Analyzer, "corpus")
	if len(results) != 1 || results[0].Err != nil {
		var err error
		if len(results) == 1 {
			err = results[0].Err
		}
		t.Fatalf("corpus results = %d, err = %v", len(results), err)
	}
	raw, err := json.Marshal(results[0].Result)
	if err != nil {
		t.Fatalf("Marshal(Result) error = %v", err)
	}
	checkSites(t, decodeSites(t, raw))
}

// TestCommandJSON ганяє census -json по дереву testdata і по пакету corpus.
func TestCommandJSON(t *testing.T) {
	testdata := analysistest.TestData()
	for _, pattern := range []string{testdata, filepath.Join(testdata, "src", "corpus")} {
		t.Run(filepath.Base(pattern), func(t *testing.T) {
			stdout, stderr, code := runCensus(t, "-json", pattern)
			if code != 0 {
				t.Fatalf("census -json %s exit = %d, want 0\nstderr:\n%s", pattern, code, stderr)
			}
			if stderr != "" {
				t.Fatalf("census -json stderr = %q, want empty", stderr)
			}
			checkSites(t, decodeSites(t, []byte(stdout)))
		})
	}
}

// TestCommandUsage: без -json, без пакета або з -h команда не друкує JSON місць.
func TestCommandUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
		code int
	}{
		{name: "no args", code: 2},
		{name: "no json", args: []string{"."}, code: 2},
		{name: "no package", args: []string{"-json"}, code: 2},
		{name: "help", args: []string{"-h"}, code: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := runCensus(t, tt.args...)
			if code != tt.code {
				t.Fatalf("census %s exit = %d, want %d\nstderr:\n%s", strings.Join(tt.args, " "), code, tt.code, stderr)
			}
			if strings.TrimSpace(stdout) != "" {
				t.Fatalf("census %s stdout = %q, want empty", strings.Join(tt.args, " "), stdout)
			}
		})
	}
}

// TestCommandEmpty: пакет без місць алокації — JSON-масив, не null.
func TestCommandEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/empty\n\ngo 1.27.0\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(go.mod) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "p.go"), []byte("package p\n\nfunc f() {}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(p.go) error = %v", err)
	}
	stdout, stderr, code := runCensus(t, "-json", dir)
	if code != 0 {
		t.Fatalf("census -json empty exit = %d, want 0\nstderr:\n%s", code, stderr)
	}
	if len(decodeSites(t, []byte(stdout))) != 0 {
		t.Fatalf("census -json empty = %s, want []", stdout)
	}
}

// TestCommandModule: каталог модуля — це всі пакети ./..., не лише корінь.
// Корінь без пакета (як у сервісі) теж входить: місця лежать у підпакеті.
func TestCommandModule(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/mod\n\ngo 1.27.0\n")
	writeFile(t, filepath.Join(dir, "q", "q.go"), "package q\n\nfunc G() *int { return new(int) }\n")

	stdout, stderr, code := runCensus(t, "-json", dir)
	if code != 0 {
		t.Fatalf("census -json module exit = %d, want 0\nstderr:\n%s", code, stderr)
	}
	got := siteAt(decodeSites(t, []byte(stdout)), "q.go:3")
	if got.Category != "rc-local" || got.Reason != "return" {
		t.Fatalf("census -json module q.go:3 = %+v, want rc-local return", got)
	}
}

// TestCommandConservative: поле any робить тип циклічним лише в консервативному
// режимі. Строгий лишає втечу поверненням і локальне new без втечі.
// Стековий Alloc лишається stack в обох режимах.
func TestCommandConservative(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/hold\n\ngo 1.27.0\n")
	writeFile(t, filepath.Join(dir, "leak.go"), "package p\n\ntype holder struct{ v any }\n\nfunc Leak() *holder { return new(holder) }\n")
	writeFile(t, filepath.Join(dir, "keep.go"), "package p\n\nfunc Keep() {\n\th := new(holder)\n\t_ = h\n}\n")
	writeFile(t, filepath.Join(dir, "stack.go"), "package p\n\nfunc Stack() {\n\tvar h holder\n\t_ = h.v\n}\n")

	strict := commandSites(t, dir)
	conservative := commandSites(t, dir, "-conservative")
	tests := []struct {
		pos                string
		strictCat, strictR string
		consCat, consR     string
	}{
		{pos: "leak.go:5", strictCat: "rc-local", strictR: "return", consCat: "cyclic", consR: "return"},
		{pos: "keep.go:4", strictCat: "static", strictR: "", consCat: "cyclic", consR: ""},
		{pos: "stack.go:4", strictCat: "stack", strictR: "", consCat: "stack", consR: ""},
	}
	for _, tt := range tests {
		gotS := siteAt(strict, tt.pos)
		if gotS.Category != tt.strictCat || gotS.Reason != tt.strictR {
			t.Errorf("strict %s = %s %q, want %s %q", tt.pos, gotS.Category, gotS.Reason, tt.strictCat, tt.strictR)
		}
		gotC := siteAt(conservative, tt.pos)
		if gotC.Category != tt.consCat || gotC.Reason != tt.consR {
			t.Errorf("conservative %s = %s %q, want %s %q", tt.pos, gotC.Category, gotC.Reason, tt.consCat, tt.consR)
		}
	}
}

func commandSites(t *testing.T, dir string, extra ...string) []site {
	t.Helper()
	args := append([]string{"-json"}, extra...)
	args = append(args, dir)
	stdout, stderr, code := runCensus(t, args...)
	if code != 0 {
		t.Fatalf("census %s exit = %d, want 0\nstderr:\n%s", strings.Join(args, " "), code, stderr)
	}
	return decodeSites(t, []byte(stdout))
}

func siteAt(sites []site, suffix string) site {
	var found site
	n := 0
	for _, s := range sites {
		if strings.HasSuffix(s.Pos, suffix) {
			found = s
			n++
		}
	}
	if n != 1 {
		return site{Pos: suffix, Category: fmt.Sprintf("count=%d", n)}
	}
	return found
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

// TestCommandLoadError: невідомий пакет не пише JSON у stdout.
func TestCommandLoadError(t *testing.T) {
	stdout, stderr, code := runCensus(t, "-json", "./no/such/package")
	if code != 1 {
		t.Fatalf("census -json missing exit = %d, want 1\nstderr:\n%s", code, stderr)
	}
	if strings.TrimSpace(stderr) == "" {
		t.Fatal("census -json missing stderr is empty, want load error")
	}
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("census -json missing stdout = %q, want empty", stdout)
	}
}

func runCensus(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	// go run підміняє ненульовий код виходу на 1. Бінарник зберігає код census.
	bin := filepath.Join(t.TempDir(), "census")
	buildArgv := []string{"build", "-o", bin, "./cmd/census"}
	build := exec.CommandContext(t.Context(), "go", buildArgv...) //nolint:gosec // G204: тест збирає census зі сталих аргументів
	build.Dir = filepath.Dir(analysistest.TestData())
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build ./cmd/census: %v\n%s", err, out)
	}
	cmd := exec.CommandContext(t.Context(), bin, args...) //nolint:gosec // G204: тест запускає census зі своїми аргументами
	cmd.Dir = filepath.Dir(analysistest.TestData())
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	if err == nil {
		return out.String(), errb.String(), 0
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("go run ./cmd/census %s error = %v\n%s", strings.Join(args, " "), err, errb.String())
	}
	return out.String(), errb.String(), exitErr.ExitCode()
}

func decodeSites(t *testing.T, raw []byte) []site {
	t.Helper()
	body := bytes.TrimSpace(raw)
	if bytes.Equal(body, []byte("null")) {
		t.Fatalf("JSON = null, want array")
	}
	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("JSON = %s, error = %v", body, err)
	}
	if rows == nil {
		t.Fatalf("JSON = %s, want array", body)
	}
	for i, row := range rows {
		for _, key := range []string{"category", "reason", "pos", "type"} {
			if _, ok := row[key]; !ok {
				t.Errorf("site %d = %v, missing %s", i, row, key)
			}
		}
	}
	var sites []site
	if err := json.Unmarshal(body, &sites); err != nil {
		t.Fatalf("JSON sites error = %v", err)
	}
	if sites == nil {
		t.Fatalf("JSON = %s, want array", body)
	}
	return sites
}

func checkSites(t *testing.T, sites []site) {
	t.Helper()
	got := map[string]int{}
	for _, s := range sites {
		if s.Category == "" || s.Type == "" || s.Pos == "" {
			t.Errorf("site %+v has empty category, pos, or type", s)
		}
		file, line, ok := strings.CutLast(s.Pos, ":")
		if !ok || filepath.Base(file) == "" || line == "" {
			t.Errorf("pos = %q, want file:line", s.Pos)
			continue
		}
		if _, err := strconv.Atoi(line); err != nil {
			t.Errorf("pos = %q, line error = %v", s.Pos, err)
		}
		got[filepath.Base(file)+":"+line+" "+siteMessage(s)]++
	}
	want := wantMessages(t)
	if maps.Equal(got, want) {
		checkSampleTypes(t, sites)
		return
	}
	keys := slices.Collect(maps.Keys(got))
	for k := range want {
		if _, ok := got[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	for _, k := range keys {
		if got[k] != want[k] {
			t.Errorf("sites %s = %d, want %d", k, got[k], want[k])
		}
	}
}

func checkSampleTypes(t *testing.T, sites []site) {
	t.Helper()
	for _, w := range sampleTypes {
		if slices.ContainsFunc(sites, func(s site) bool {
			file, line, _ := strings.CutLast(s.Pos, ":")
			return filepath.Base(file) == w.file && line == strconv.Itoa(w.line) &&
				s.Category == w.category && s.Reason == w.reason && s.Type == w.typ
		}) {
			continue
		}
		t.Errorf("missing %s:%d %s reason %q type %s", w.file, w.line, w.category, w.reason, w.typ)
	}
}

func siteMessage(s site) string {
	if s.Reason == "" {
		return s.Category
	}
	return s.Category + ": " + s.Reason
}

func wantMessages(t *testing.T) map[string]int {
	t.Helper()
	dir := filepath.Join(analysistest.TestData(), "src", "corpus")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s) error = %v", dir, err)
	}
	counts := map[string]int{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("ReadFile(%s) error = %v", entry.Name(), err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			for _, msg := range wantTexts(line) {
				counts[fmt.Sprintf("%s:%d %s", entry.Name(), i+1, msg)]++
			}
		}
	}
	return counts
}

func wantTexts(line string) []string {
	_, rest, ok := strings.Cut(line, "// want")
	if !ok {
		return nil
	}
	var out []string
	for i := 0; i < len(rest); i++ {
		if rest[i] != '"' && rest[i] != '`' {
			continue
		}
		quote := rest[i]
		i++
		start := i
		for i < len(rest) && rest[i] != quote {
			if rest[i] == '\\' && quote == '"' {
				i++
			}
			i++
		}
		text := strings.TrimPrefix(rest[start:i], "^")
		text = strings.TrimSuffix(text, "$")
		out = append(out, text)
	}
	return out
}

// sampleTypes — типи значень SSA, звірені з фікстурами.
// Стала ємність make знижується до new([n]T), тож тип — вказівник на масив.
var sampleTypes = []struct {
	file     string
	line     int
	category string
	reason   string
	typ      string
}{
	{"static.go", 5, "static", "", "*int"},
	{"static.go", 12, "static", "", "*[3]int"},
	{"static.go", 18, "static", "", "[]int"},
	{"static.go", 26, "static", "", "map[string]int"},
	{"static.go", 32, "static", "", "chan int"},
	{"static.go", 38, "static", "", "any"},
	{"static.go", 51, "rc-local", "closure", "*int"},
	{"static.go", 52, "static", "", "func() int"},
	{"stack.go", 8, "stack", "", "*[4]int"},
	{"implicit.go", 6, "implicit", "append", "[]int"},
	{"implicit.go", 6, "static", "", "*[1]int"},
	{"implicit.go", 25, "implicit", "concat", "string"},
	{"implicit.go", 34, "implicit", "string", "string"},
	{"implicit.go", 38, "implicit", "string", "[]byte"},
	{"implicit.go", 42, "implicit", "string", "[]rune"},
	{"cyclic.go", 7, "cyclic", "", "*list"},
	{"pinned.go", 6, "pinned", "unsafe", "*int"},
	{"pinned.go", 18, "pinned", "unsafe", "*[8]byte"},
	{"pinned.go", 24, "pinned", "unsafe", "*struct{f int}"},
	{"pinned.go", 30, "pinned", "unsafe", "[]int"},
	{"rcshared.go", 6, "rc-shared", "global", "*int"},
	{"rcshared.go", 27, "rc-shared", "global", "[]int"},
}
