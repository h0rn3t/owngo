package census

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	json "encoding/json/v2"
)

// gcLineRE — file:line:col: message. Повідомлення може містити двокрапку
// («moved to heap: a»), тож колонка береться регулярним виразом, не CutLast.
var gcLineRE = regexp.MustCompile(`^(.+):(\d+):(\d+): (.*)$`)

// GCDiag — діагностика escape analysis gc для одного рядка файла.
// Heap означає втечу в купу («escapes to heap», «moved to heap»).
// Stack означає алокацію, яку gc не виводить у купу («does not escape»
// для виразу, не для голого ідентифікатора на кшталт параметра).
type GCDiag struct {
	File  string
	Line  int
	Heap  bool
	Stack bool
}

// StackGaps повертає місця, які go/ssa кладе в купу (категорія не stack),
// а gc на тому самому рядку звітує як алокацію, що не тікає.
// Рядок, де є і stack-, і heap-діагностика, розривом не є.
// Канал gc не діагностує і завжди лишає в купі, тож без діагностики
// місце розривом не стає. «does not escape» для make зі змінним розміром
// лишає на стеку лише об'єкт до порога компілятора.
func StackGaps(sites []Site, diags []GCDiag) []Site {
	type fact struct{ heap, stack bool }
	byFile := map[string]map[int]*fact{}
	for _, diag := range diags {
		file := filepath.Clean(diag.File)
		lines := byFile[file]
		if lines == nil {
			lines = map[int]*fact{}
			byFile[file] = lines
		}
		got := lines[diag.Line]
		if got == nil {
			got = &fact{}
			lines[diag.Line] = got
		}
		got.heap = got.heap || diag.Heap
		got.stack = got.stack || diag.Stack
	}

	out := []Site{}
	for _, site := range sites {
		if site.Category == "stack" {
			continue
		}
		file, line, ok := splitPos(site.Pos)
		if !ok {
			continue
		}
		var heap, stack bool
		for diagFile, lines := range byFile {
			if !sameFile(file, diagFile) {
				continue
			}
			got := lines[line]
			if got == nil {
				continue
			}
			heap = heap || got.heap
			stack = stack || got.stack
		}
		if stack && !heap {
			out = append(out, site)
		}
	}
	return out
}

// ParseGCText читає stderr `go build -gcflags=-m`.
// Рядки без позиції файла ігноруються.
func ParseGCText(r io.Reader) ([]GCDiag, error) {
	out := []GCDiag{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		file, line, msg, ok := splitGCLine(sc.Text())
		if !ok {
			continue
		}
		heap, stack := classifyGC(msg)
		if !heap && !stack {
			continue
		}
		out = append(out, GCDiag{File: file, Line: line, Heap: heap, Stack: stack})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ParseGCJSON читає каталог `-json=0,file://…`.
// Коди escape і escapes позначають купу. Номер рядка такий самий, як у -m.
func ParseGCJSON(dir string) ([]GCDiag, error) {
	out := []GCDiag{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		diags, err := parseGCJSONFile(path)
		if err != nil {
			return err
		}
		out = append(out, diags...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RunGC збирає patterns у dir командою
// `go build -gcflags=-m -json=0,file://…` і повертає діагностики -m і JSON.
// extraEnv додається в кінець оточення і перекриває однойменні змінні.
func RunGC(dir string, patterns []string, extraEnv []string) ([]GCDiag, error) {
	if len(patterns) == 0 {
		return nil, fmt.Errorf("немає пакета для gc")
	}
	jsonDir, err := os.MkdirTemp("", "census-gc-")
	if err != nil {
		return nil, err
	}
	defer removeTemp(jsonDir)

	pkgs, err := gcPackages(dir, patterns, extraEnv)
	if err != nil {
		return nil, err
	}
	text, err := compileGC(dir, jsonDir, pkgs, extraEnv)
	if err != nil {
		return nil, err
	}
	textDiags, err := ParseGCText(strings.NewReader(text))
	if err != nil {
		return nil, err
	}
	jsonDiags, err := ParseGCJSON(jsonDir)
	if err != nil {
		return nil, err
	}
	return append(textDiags, jsonDiags...), nil
}

func gcPackages(dir string, patterns, extraEnv []string) ([][2]string, error) {
	args := append([]string{"list", "-f", "{{.ImportPath}}\t{{.Name}}"}, patterns...)
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), extraEnv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go list: %w\n%s", err, stderr.String())
	}
	var pkgs [][2]string
	for line := range strings.SplitSeq(strings.TrimSpace(stdout.String()), "\n") {
		if line == "" {
			continue
		}
		path, name, ok := strings.Cut(line, "\t")
		if !ok || path == "" {
			return nil, fmt.Errorf("go list: рядок %q", line)
		}
		pkgs = append(pkgs, [2]string{path, name})
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("go list: порожньо")
	}
	return pkgs, nil
}

func compileGC(dir, jsonDir string, pkgs [][2]string, extraEnv []string) (string, error) {
	binDir, err := os.MkdirTemp("", "census-gc-bin-")
	if err != nil {
		return "", err
	}
	defer removeTemp(binDir)

	flag := "-gcflags=-m -json=0,file://" + jsonDir
	var stderr bytes.Buffer
	for _, pkg := range pkgs {
		args := []string{"build", flag}
		if pkg[1] == "main" {
			args = append(args, "-o", filepath.Join(binDir, "bin"))
		}
		args = append(args, pkg[0])
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), extraEnv...)
		cmd.Stdout = &stderr
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("go build %s: %w\n%s", pkg[0], err, stderr.String())
		}
	}
	return stderr.String(), nil
}

func parseGCJSONFile(path string) (out []GCDiag, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()

	out = []GCDiag{}
	file := ""
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var line gcJSONLine
		if err = json.Unmarshal(sc.Bytes(), &line); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if line.Version != nil {
			file = line.File
			continue
		}
		if file == "" || (line.Code != "escape" && line.Code != "escapes") {
			continue
		}
		out = append(out, GCDiag{File: file, Line: line.Range.Start.Line, Heap: true})
	}
	if err = sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func removeTemp(dir string) {
	_ = os.RemoveAll(dir) // каталог вже прочитано; помилка прибирання не змінює діагностики
}

type gcJSONLine struct {
	Version *int   `json:"version"`
	File    string `json:"file"`
	Code    string `json:"code"`
	Range   struct {
		Start struct {
			Line int `json:"line"`
		} `json:"start"`
	} `json:"range"`
}

func splitGCLine(text string) (file string, line int, msg string, ok bool) {
	m := gcLineRE.FindStringSubmatch(text)
	if m == nil {
		return "", 0, "", false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil || m[1] == "" {
		return "", 0, "", false
	}
	return m[1], n, m[4], true
}

func classifyGC(msg string) (heap, stack bool) {
	if strings.Contains(msg, "escapes to heap") || strings.HasPrefix(msg, "moved to heap:") {
		return true, false
	}
	subject, ok := strings.CutSuffix(msg, " does not escape")
	if !ok || subject == "" {
		return false, false
	}
	if ident(subject) && subject != "append" {
		return false, false
	}
	return false, true
}

func ident(s string) bool {
	for i, r := range s {
		if r == '_' || unicode.IsLetter(r) || (i > 0 && unicode.IsDigit(r)) {
			continue
		}
		return false
	}
	return s != ""
}

func splitPos(pos string) (string, int, bool) {
	file, lineStr, ok := strings.CutLast(pos, ":")
	if !ok || file == "" {
		return "", 0, false
	}
	line, err := strconv.Atoi(lineStr)
	if err != nil {
		return "", 0, false
	}
	return file, line, true
}

func sameFile(siteFile, diagFile string) bool {
	siteFile = filepath.Clean(siteFile)
	diagFile = filepath.Clean(diagFile)
	if siteFile == diagFile {
		return true
	}
	sep := string(filepath.Separator)
	return strings.HasSuffix(siteFile, sep+diagFile) || strings.HasSuffix(diagFile, sep+siteFile)
}
