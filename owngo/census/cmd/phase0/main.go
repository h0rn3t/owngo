// Команда phase0 зводить один модуль для звіту фази 0:
// категорії v0, консервативний режим інтерфейсів, розрив із gc
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
	strict, err := classify(roots, false)
	if err != nil {
		return err
	}
	conservative, err := classify(roots, true)
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

	gaps, gapErrs := stackGaps(*dir, roots, strict)
	var weights, weightsCons []census.Weight
	if *profDir != "" {
		samples, err := readProfiles(*profDir)
		if err != nil {
			return err
		}
		weights = census.Weigh(strict, samples)
		weightsCons = census.Weigh(conservative, samples)
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
		"gaps":                 summary(gaps),
		"gap_errors":           gapErrs,
		"weights":              weights,
		"weights_conservative": weightsCons,
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

func classify(pkgs []*packages.Package, conservative bool) ([]census.Site, error) {
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
