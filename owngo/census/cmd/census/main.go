// Command census prints allocation sites of Go packages as JSON.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	json "encoding/json/v2"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/checker"
	"golang.org/x/tools/go/packages"

	"github.com/xgo-dev/llgo/owngo/census"
)

// errUsage — виклик без -json або без пакета. main завершується з кодом 2.
var errUsage = errors.New("usage")

func main() {
	err := run(os.Args[1:], os.Stdout, os.Stderr)
	if err == nil || errors.Is(err, flag.ErrHelp) {
		os.Exit(0)
	}
	if errors.Is(err, errUsage) {
		os.Exit(2)
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("census", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOut := fs.Bool("json", false, "print allocation sites as JSON")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: census -json <package>")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return errUsage
	}
	if !*jsonOut || fs.NArg() == 0 {
		fs.Usage()
		return errUsage
	}

	pkgs, err := load(fs.Args())
	if err != nil {
		return err
	}
	graph, err := checker.Analyze([]*analysis.Analyzer{census.Analyzer}, pkgs, nil)
	if err != nil {
		return err
	}
	sites, err := sitesFrom(graph)
	if err != nil {
		return err
	}
	if err := json.MarshalWrite(stdout, sites); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout); err != nil {
		return err
	}
	return nil
}

// load вантажить пакети. Каталог testdata go list не бачить, тож такий
// шлях читається як GOPATH із пакетами під src.
func load(patterns []string) ([]*packages.Package, error) {
	if len(patterns) == 1 {
		if pkgs, ok, err := loadTestdata(patterns[0]); ok {
			return pkgs, err
		}
		// Каталог поза поточним модулем go list бачить лише якщо запит іде з нього.
		if info, err := os.Stat(patterns[0]); err == nil && info.IsDir() {
			pkgs, err := packages.Load(&packages.Config{Mode: packages.LoadAllSyntax, Dir: patterns[0]}, ".")
			if err != nil {
				return nil, err
			}
			if err := packageErrors(pkgs); err != nil {
				return nil, err
			}
			return pkgs, nil
		}
	}
	pkgs, err := packages.Load(&packages.Config{Mode: packages.LoadAllSyntax}, patterns...)
	if err != nil {
		return nil, err
	}
	if err := packageErrors(pkgs); err != nil {
		return nil, err
	}
	return pkgs, nil
}

func loadTestdata(pattern string) ([]*packages.Package, bool, error) {
	info, err := os.Stat(pattern)
	if err != nil || !info.IsDir() {
		return nil, false, nil
	}
	gopath, imports, ok := testdataLayout(pattern)
	if !ok {
		return nil, false, nil
	}
	if imports == nil {
		imports, err = gopathImports(gopath)
		if err != nil {
			return nil, true, err
		}
	}
	env := gopathEnv(gopath)
	cfg := &packages.Config{
		Mode: packages.LoadAllSyntax,
		Dir:  gopath,
		Env:  env,
	}
	pkgs, err := packages.Load(cfg, imports...)
	if err != nil {
		return nil, true, err
	}
	if err := packageErrors(pkgs); err != nil {
		return nil, true, err
	}
	return pkgs, true, nil
}

// testdataLayout відділяє корінь GOPATH (каталог testdata) від шляху імпорту.
// Порожній список імпортів означає всі пакети під src.
func testdataLayout(dir string) (gopath string, imports []string, ok bool) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", nil, false
	}
	abs = filepath.Clean(abs)
	parts := strings.Split(abs, string(filepath.Separator))
	idx := -1
	for i, part := range parts {
		if part == "testdata" {
			idx = i
		}
	}
	if idx < 0 {
		return "", nil, false
	}
	gopath = strings.Join(parts[:idx+1], string(filepath.Separator))
	rest := parts[idx+1:]
	if len(rest) == 0 || (len(rest) == 1 && rest[0] == "src") {
		return gopath, nil, true
	}
	if rest[0] != "src" || len(rest) < 2 {
		return "", nil, false
	}
	return gopath, []string{filepath.ToSlash(filepath.Join(rest[1:]...))}, true
}

func gopathImports(gopath string) ([]string, error) {
	src := filepath.Join(gopath, "src")
	var imports []string
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() || path == src {
			return nil
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
				continue
			}
			rel, err := filepath.Rel(src, path)
			if err != nil {
				return err
			}
			imports = append(imports, filepath.ToSlash(rel))
			return nil
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(imports) == 0 {
		return nil, fmt.Errorf("у %s немає пакетів", src)
	}
	slices.Sort(imports)
	return imports, nil
}

func gopathEnv(gopath string) []string {
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		switch key {
		case "GO111MODULE", "GOPATH", "GOWORK":
			continue
		}
		env = append(env, kv)
	}
	return append(env, "GO111MODULE=off", "GOPATH="+gopath, "GOWORK=off")
}

func packageErrors(pkgs []*packages.Package) error {
	var errs []error
	for _, pkg := range pkgs {
		for _, pkgErr := range pkg.Errors {
			errs = append(errs, errors.New(pkgErr.Error()))
		}
	}
	if len(pkgs) == 0 {
		errs = append(errs, errors.New("пакетів не знайдено"))
	}
	return errors.Join(errs...)
}

func sitesFrom(graph *checker.Graph) ([]census.Site, error) {
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
