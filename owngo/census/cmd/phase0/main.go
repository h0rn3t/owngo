// Команда phase0 зводить один модуль для звіту фази 0:
// категорії v0 і v1, консервативний режим інтерфейсів, розрив із gc
// і ваги профілю алокацій, якщо каталог профілів задано.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	json "encoding/json/v2"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/checker"
	"golang.org/x/tools/go/packages"

	"github.com/xgo-dev/llgo/owngo/census"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("phase0", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "module directory")
	profDir := fs.String("profdir", "", "directory of alloc profiles")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" || fs.NArg() != 0 {
		return errors.New("usage: phase0 -dir module [-profdir profiles]")
	}

	roots, skipped, err := loadRoots(*dir)
	if err != nil {
		return err
	}
	strict, err := classify(roots, false, false)
	if err != nil {
		return err
	}
	conservative, err := classify(roots, true, false)
	if err != nil {
		return err
	}
	if len(strict) != len(conservative) {
		return fmt.Errorf("місць strict %d, conservative %d", len(strict), len(conservative))
	}
	changed := 0
	for i := range strict {
		if strict[i].Category != conservative[i].Category || strict[i].Reason != conservative[i].Reason {
			changed++
		}
	}
	v1Sites, err := classify(roots, false, true)
	if err != nil {
		return err
	}
	if len(strict) != len(v1Sites) {
		return fmt.Errorf("місць strict %d, v1 %d", len(strict), len(v1Sites))
	}
	v1Changed := 0
	for i := range strict {
		if strict[i].Category != v1Sites[i].Category || strict[i].Reason != v1Sites[i].Reason {
			v1Changed++
		}
	}

	gaps, gapErrs := stackGaps(*dir, roots, strict)
	var weights, weightsCons, weightsV1 []census.Weight
	if *profDir != "" {
		samples, err := readProfiles(*profDir)
		if err != nil {
			return err
		}
		weights = census.Weigh(strict, samples)
		weightsCons = census.Weigh(conservative, samples)
		weightsV1 = census.Weigh(v1Sites, samples)
	}

	rev := gitRev(*dir)
	out := map[string]any{
		"dir":                  *dir,
		"rev":                  rev,
		"packages":             len(roots),
		"skipped":              skipped,
		"strict":               summary(strict),
		"conservative":         summary(conservative),
		"category_changed":     changed,
		"v1":                   summary(v1Sites),
		"v1_changed":           v1Changed,
		"gaps":                 summary(gaps),
		"gap_errors":           gapErrs,
		"weights":              weights,
		"weights_conservative": weightsCons,
		"weights_v1":           weightsV1,
	}
	if err := json.MarshalWrite(stdout, out); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout); err != nil {
		return err
	}
	return nil
}

func loadRoots(dir string) ([]*packages.Package, []string, error) {
	pkgs, err := packages.Load(&packages.Config{Mode: packages.LoadAllSyntax, Dir: dir}, "./...")
	if err != nil {
		return nil, nil, err
	}
	var roots []*packages.Package
	var skipped []string
	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 {
			skipped = append(skipped, pkg.PkgPath)
			continue
		}
		roots = append(roots, pkg)
	}
	slices.Sort(skipped)
	return roots, skipped, nil
}

func classify(pkgs []*packages.Package, conservative, v1 bool) ([]census.Site, error) {
	if census.Analyzer.Flags.Lookup("conservative") == nil {
		census.Analyzer.Flags.Bool("conservative", false, "interface edges count as cycles")
	}
	value := "false"
	if conservative {
		value = "true"
	}
	if err := census.Analyzer.Flags.Set("conservative", value); err != nil {
		return nil, err
	}
	on := "false"
	if v1 {
		on = "true"
	}
	if err := census.Analyzer.Flags.Set("v1", on); err != nil {
		return nil, err
	}
	prevFacts := census.Analyzer.FactTypes
	// v1 не вмикає FactTypes: інакше checker будує SSA всього графа імпорту.
	// Зведення між пакетами модуля йдуть через census.ResetSummaries і topo.
	census.Analyzer.FactTypes = nil
	defer func() {
		census.Analyzer.FactTypes = prevFacts
		_ = census.Analyzer.Flags.Set("v1", "false")
	}()
	if !v1 {
		return analyze(pkgs)
	}
	census.ResetSummaries()
	defer census.ResetSummaries()
	byPath := map[string][]census.Site{}
	for _, pkg := range orderPackages(pkgs) {
		got, err := analyze([]*packages.Package{pkg})
		if err != nil {
			return nil, err
		}
		byPath[pkg.PkgPath] = got
	}
	// Той самий порядок пакетів, що й у analyze, інакше v1_changed звіряє чужі місця.
	ordered := slices.Clone(pkgs)
	slices.SortFunc(ordered, func(a, b *packages.Package) int {
		return strings.Compare(a.PkgPath, b.PkgPath)
	})
	var sites []census.Site
	for _, pkg := range ordered {
		sites = append(sites, byPath[pkg.PkgPath]...)
	}
	return sites, nil
}

func analyze(pkgs []*packages.Package) ([]census.Site, error) {
	graph, err := checker.Analyze([]*analysis.Analyzer{census.Analyzer}, pkgs, nil)
	if err != nil {
		return nil, err
	}
	roots := slices.Clone(graph.Roots)
	slices.SortFunc(roots, func(a, b *checker.Action) int {
		return strings.Compare(a.Package.PkgPath, b.Package.PkgPath)
	})
	sites := []census.Site{}
	for _, root := range roots {
		if root.Err != nil {
			return nil, fmt.Errorf("%s: %w", root.Package.PkgPath, root.Err)
		}
		got, ok := root.Result.([]census.Site)
		if !ok {
			return nil, fmt.Errorf("%s: результат census має тип %T", root.Package.PkgPath, root.Result)
		}
		sites = append(sites, got...)
	}
	return sites, nil
}

func orderPackages(pkgs []*packages.Package) []*packages.Package {
	byPath := map[string]*packages.Package{}
	indeg := map[string]int{}
	users := map[string][]string{}
	paths := make([]string, 0, len(pkgs))
	for _, pkg := range pkgs {
		byPath[pkg.PkgPath] = pkg
		paths = append(paths, pkg.PkgPath)
	}
	slices.Sort(paths)
	for _, path := range paths {
		pkg := byPath[path]
		for imp := range pkg.Imports {
			if _, ok := byPath[imp]; !ok {
				continue
			}
			users[imp] = append(users[imp], path)
			indeg[path]++
		}
	}
	var ready []string
	for _, path := range paths {
		if indeg[path] == 0 {
			ready = append(ready, path)
		}
	}
	ordered := make([]string, 0, len(paths))
	for len(ready) > 0 {
		slices.Sort(ready)
		path := ready[0]
		ready = ready[1:]
		ordered = append(ordered, path)
		next := slices.Clone(users[path])
		slices.Sort(next)
		for _, user := range next {
			indeg[user]--
			if indeg[user] == 0 {
				ready = append(ready, user)
			}
		}
	}
	if len(ordered) != len(paths) {
		return pkgs
	}
	out := make([]*packages.Package, len(ordered))
	for i, path := range ordered {
		out[i] = byPath[path]
	}
	return out
}

func stackGaps(dir string, pkgs []*packages.Package, sites []census.Site) ([]census.Site, []string) {
	patterns := make([]string, 0, len(pkgs))
	for _, pkg := range pkgs {
		patterns = append(patterns, pkg.PkgPath)
	}
	diags, err := census.RunGC(dir, patterns, nil)
	if err == nil {
		return census.StackGaps(sites, diags), nil
	}
	var all []census.GCDiag
	var errs []string
	for _, pattern := range patterns {
		got, err := census.RunGC(dir, []string{pattern}, nil)
		if err != nil {
			errs = append(errs, pattern+": "+err.Error())
			continue
		}
		all = append(all, got...)
	}
	return census.StackGaps(sites, all), errs
}

func summary(sites []census.Site) map[string]any {
	cats := map[string]int{}
	reasons := map[string]int{}
	for _, site := range sites {
		cats[site.Category]++
		reason := site.Reason
		if reason == "" {
			reason = "-"
		}
		reasons[site.Category+":"+reason]++
	}
	return map[string]any{
		"sites":      len(sites),
		"categories": ordered(cats),
		"reasons":    ordered(reasons),
	}
}

func ordered(counts map[string]int) [][2]any {
	keys := slices.Sorted(maps.Keys(counts))
	out := make([][2]any, 0, len(keys))
	for _, key := range keys {
		out = append(out, [2]any{key, counts[key]})
	}
	return out
}

func readProfiles(dir string) ([]census.AllocSample, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var samples []census.AllocSample
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		path := filepath.Join(dir, name)
		var raw []byte
		switch {
		case strings.HasSuffix(name, ".raw"):
			raw, err = os.ReadFile(path)
		case strings.HasSuffix(name, ".out"):
			cmd := exec.Command("go", "tool", "pprof", "-raw", path)
			raw, err = cmd.Output()
		default:
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		got, err := census.ParseAllocProfile(bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		samples = append(samples, got...)
	}
	return samples, nil
}

func gitRev(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
