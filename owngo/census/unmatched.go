package census

import (
	"cmp"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// OutsideHarness — вибірка обв'язки тесту: стек проходить через пакет
// testing, _testmain.go або *_test.go і не заходить у нетестовий файл
// каталогу -dir. У бінарник програми такі алокації не входять.
const OutsideHarness = "harness"

// UnmatchedSplit — розклад рядка Unmatched з Weigh за причиною.
type UnmatchedSplit struct {
	// Reasons — причини в порядку harness, runtime, stdlib, external,
	// module. Сума дорівнює рядку Unmatched.
	Reasons []Weight
	// Stdlib — вибірки причини stdlib, з'єднані з місцями v1 пакетів
	// stdlib, як StdlibCategories.
	Stdlib []Weight
	// Packages — імпорти пакетів stdlib поза runtime, через кадри яких
	// проходять вибірки причини stdlib: їхні місця v1 потрібні для Stdlib.
	Packages []string
	// Lines — вибірки причини module за функцією і рядком першого кадру
	// поза runtime, від більших байтів до менших.
	Lines []LineWeight
}

// LineWeight — вибірки одного рядка файлу, на якому census не має місця.
type LineWeight struct {
	Func    string
	Pos     string
	Objects int64
	Bytes   int64
}

// UnmatchedReasons розкладає вибірки, які Weigh лишає без місця в sites.
// Причину дає перший кадр поза runtime, як в Outside, крім обв'язки тесту:
// її визначає весь стек (OutsideHarness), тож stdlib, який кличе лише
// testing, теж обв'язка. Вибірки stdlib з'єднуються з stdlibSites.
func UnmatchedReasons(sites, stdlibSites []Site, samples []AllocSample, moduleDir, goroot string) UnmatchedSplit {
	module := resolvePath(moduleDir)
	root := resolvePath(goroot)
	testingDir := filepath.Join(root, "src", "testing")

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
	var stdlib []AllocSample
	packages := map[string]bool{}
	lines := map[[2]string][2]int64{}
	walkOutside(sites, samples, moduleDir, goroot, func(kind, _ string, sample AllocSample) {
		harness, program := false, false
		leaf := -1
		for i, frame := range sample.Frames {
			file := resolve(frame.File)
			if runtimeFile(file, root) {
				continue
			}
			if leaf < 0 {
				leaf = i
			}
			base := filepath.Base(file)
			switch {
			case strings.HasSuffix(base, "_test.go") || base == "_testmain.go" || under(file, testingDir):
				harness = true
			case under(file, module):
				program = true
			}
		}
		if kind == OutsideTest || harness && !program {
			kind = OutsideHarness
		}
		cur := count[kind]
		cur[0] += sample.Objects
		cur[1] += sample.Bytes
		count[kind] = cur

		switch kind {
		case OutsideStdlib:
			stdlib = append(stdlib, sample)
			for _, frame := range sample.Frames {
				file := resolve(frame.File)
				if pkg := stdlibImport(file, root); pkg != "" && !runtimeFile(file, root) {
					packages[pkg] = true
				}
			}
		case OutsideModule:
			frame := sample.Frames[leaf]
			key := [2]string{frame.Func, frame.File + ":" + strconv.Itoa(frame.Line)}
			cur := lines[key]
			cur[0] += sample.Objects
			cur[1] += sample.Bytes
			lines[key] = cur
		}
	})

	split := UnmatchedSplit{
		Reasons:  []Weight{},
		Stdlib:   Weigh(stdlibSites, stdlib),
		Packages: slices.Sorted(maps.Keys(packages)),
		Lines:    []LineWeight{},
	}
	for _, reason := range []string{OutsideHarness, OutsideRuntime, OutsideStdlib, OutsideExternal, OutsideModule} {
		cur := count[reason]
		if cur[0] == 0 && cur[1] == 0 {
			continue
		}
		split.Reasons = append(split.Reasons, Weight{Category: reason, Objects: cur[0], Bytes: cur[1]})
	}
	for key, cur := range lines {
		split.Lines = append(split.Lines, LineWeight{Func: key[0], Pos: key[1], Objects: cur[0], Bytes: cur[1]})
	}
	slices.SortFunc(split.Lines, func(a, b LineWeight) int {
		return cmp.Or(
			cmp.Compare(b.Bytes, a.Bytes),
			cmp.Compare(b.Objects, a.Objects),
			cmp.Compare(a.Pos, b.Pos),
			cmp.Compare(a.Func, b.Func),
		)
	})
	return split
}
