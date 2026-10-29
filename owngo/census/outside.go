package census

import (
	"path/filepath"
	"strings"
)

const (
	// OutsideStdlib — вибірка поза census, чий перший кадр після runtime
	// лежить у стандартній бібліотеці.
	OutsideStdlib = "stdlib"
	// OutsideExternal — вибірка поза census у модулі поза каталогом -dir.
	OutsideExternal = "external"
	// OutsideTest — вибірка поза census у файлі *_test.go або в _testmain.go.
	OutsideTest = "test"
	// OutsideRuntime — вибірка поза census, у стеку якої лишився лише пакет runtime.
	OutsideRuntime = "runtime"
	// OutsideModule — вибірка поза census у нетестовому файлі каталогу -dir:
	// рядок не збігся з місцем.
	OutsideModule = "module"
)

// Outside розкладає вибірки, які Weigh лишає поза census.
// moduleDir — каталог -dir, goroot — корінь GOROOT, з якого зібрано профіль.
// Кадр пакета runtime (не runtime/pprof і не інші бібліотечні підпакети)
// провалюється до наступного. Порожній вхід дає порожній зріз, не nil.
// Сума об'єктів і байтів дорівнює рядку Unmatched у Weigh.
func Outside(sites []Site, samples []AllocSample, moduleDir, goroot string) []Weight {
	moduleDir = resolvePath(moduleDir)
	goroot = resolvePath(goroot)

	type placed struct {
		file string
		line int
		ok   bool
	}
	where := make([]placed, len(sites))
	type lineKey struct {
		base string
		line int
	}
	index := map[lineKey][]int{}
	for i, site := range sites {
		file, line, ok := splitPos(site.Pos)
		where[i] = placed{file: file, line: line, ok: ok}
		if !ok {
			continue
		}
		key := lineKey{base: filepath.Base(file), line: line}
		index[key] = append(index[key], i)
	}

	seen := map[string]string{}
	resolve := func(path string) string {
		if got, ok := seen[path]; ok {
			return got
		}
		got := resolvePath(path)
		seen[path] = got
		return got
	}
	count := map[string][2]int64{}
	for _, sample := range samples {
		if sample.Objects == 0 && sample.Bytes == 0 {
			continue
		}
		matched := false
		for _, frame := range sample.Frames {
			cands := index[lineKey{base: filepath.Base(frame.File), line: frame.Line}]
			for _, i := range cands {
				if where[i].ok && where[i].line == frame.Line && sameFile(where[i].file, frame.File) {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if matched {
			continue
		}
		files := make([]string, len(sample.Frames))
		for i, frame := range sample.Frames {
			files[i] = resolve(frame.File)
		}
		kind := outsideKind(files, moduleDir, goroot)
		cur := count[kind]
		cur[0] += sample.Objects
		cur[1] += sample.Bytes
		count[kind] = cur
	}

	out := []Weight{}
	for _, cat := range []string{OutsideStdlib, OutsideExternal, OutsideTest, OutsideRuntime, OutsideModule} {
		cur := count[cat]
		if cur[0] == 0 && cur[1] == 0 {
			continue
		}
		out = append(out, Weight{Category: cat, Objects: cur[0], Bytes: cur[1]})
	}
	return out
}

func outsideKind(files []string, moduleDir, goroot string) string {
	sawRuntime := false
	for _, file := range files {
		if runtimeFile(file, goroot) {
			sawRuntime = true
			continue
		}
		base := filepath.Base(file)
		if strings.HasSuffix(base, "_test.go") || base == "_testmain.go" {
			return OutsideTest
		}
		if under(file, filepath.Join(goroot, "src")) {
			return OutsideStdlib
		}
		if under(file, moduleDir) {
			return OutsideModule
		}
		return OutsideExternal
	}
	if sawRuntime {
		return OutsideRuntime
	}
	return OutsideExternal
}

func runtimeFile(file, goroot string) bool {
	root := filepath.Join(goroot, "src", "runtime")
	if !under(file, root) {
		return false
	}
	rel, err := filepath.Rel(root, file)
	if err != nil {
		return false
	}
	top, _, nested := strings.Cut(rel, string(filepath.Separator))
	if !nested {
		return true
	}
	switch top {
	case "internal", "cgo", "race", "msan", "asan", "secret":
		return true
	default:
		return false
	}
}

func under(file, dir string) bool {
	if dir == "" {
		return false
	}
	dir = filepath.Clean(dir)
	file = filepath.Clean(file)
	return file == dir || strings.HasPrefix(file, dir+string(filepath.Separator))
}

func resolvePath(path string) string {
	if path == "" || !filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return resolved
}
