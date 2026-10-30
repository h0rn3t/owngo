package census

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestOutsideEmpty(t *testing.T) {
	got := Outside(nil, nil, "", "")
	if got == nil || len(got) != 0 {
		t.Errorf("Outside(nil, nil) = %#v, want empty slice", got)
	}
}

func TestOutsideProfile(t *testing.T) {
	const (
		module = "/app"
		goroot = "/goroot"
	)
	raw := `
Samples:
alloc_objects/count alloc_space/bytes
         10        100: 1
         20        200: 2
         30        300: 3
         40        400: 4
         50        500: 5
          1          7: 6
          2          9: 7
        100       1000: 8
          0          0: 2
          3         11: 9
          4         13: 10
          5         17: 11
Locations
     1: 0x1 M=1 bytes.growSlice /goroot/src/bytes/buffer.go:1:0 s=1
     2: 0x2 M=1 mod.F /Users/x/go/pkg/mod/github.com/foo@v1.0.0/a.go:2:0 s=1
     3: 0x3 M=1 app.TestF /app/a_test.go:3:0 s=1
     4: 0x4 M=1 runtime.mallocgc /goroot/src/runtime/malloc.go:4:0 s=1
     5: 0x5 M=1 runtime.mallocgc /goroot/src/runtime/malloc.go:4:0 s=1
             fmt.Sprintf /goroot/src/fmt/print.go:5:0 s=1
     6: 0x6 M=1 fmt.TestPrint /goroot/src/fmt/print_test.go:6:0 s=1
     7: 0x7 M=1 app.miss /app/miss.go:7:0 s=1
     8: 0x8 M=1 runtime.mallocgc /goroot/src/runtime/malloc.go:4:0 s=1
             app.hit /app/hit.go:4:0 s=1
     9: 0x9 M=1 main.main _testmain.go:9:0 s=1
    10: 0xa M=1 runtime/pprof.(*Profile).WriteTo /goroot/src/runtime/pprof/pprof.go:10:0 s=1
    11: 0xb M=1 dep.TestF /Users/x/go/pkg/mod/github.com/foo@v1.0.0/a_test.go:11:0 s=1
             runtime.growslice /goroot/src/runtime/slice.go:12:0 s=1
Mappings
`
	samples, err := ParseAllocProfile(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseAllocProfile() error = %v, want nil", err)
	}
	sites := []Site{{Category: "static", Pos: module + "/hit.go:4"}}
	got := Outside(sites, samples, module, goroot)
	want := []Weight{
		{Category: OutsideStdlib, Objects: 64, Bytes: 613},
		{Category: OutsideExternal, Objects: 20, Bytes: 200},
		{Category: OutsideTest, Objects: 39, Bytes: 335},
		{Category: OutsideRuntime, Objects: 40, Bytes: 400},
		{Category: OutsideModule, Objects: 2, Bytes: 9},
	}
	if fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", want) {
		t.Errorf("Outside(profile) = %#v, want %#v", got, want)
	}

	var outsideObjects, outsideBytes int64
	for _, weight := range got {
		outsideObjects += weight.Objects
		outsideBytes += weight.Bytes
	}
	var matchedUnmatched bool
	for _, weight := range Weigh(sites, samples) {
		if weight.Category != Unmatched {
			continue
		}
		matchedUnmatched = true
		if weight.Objects != outsideObjects || weight.Bytes != outsideBytes {
			t.Errorf("Outside sum = %d objects, %d bytes, want Weigh unmatched %d, %d",
				outsideObjects, outsideBytes, weight.Objects, weight.Bytes)
		}
	}
	if !matchedUnmatched {
		t.Errorf("Weigh unmatched missing, Outside sum = %d objects, %d bytes", outsideObjects, outsideBytes)
	}
}

func TestStdlibPackagesEmpty(t *testing.T) {
	got := StdlibPackages(nil, nil, "", "")
	if got == nil || len(got) != 0 {
		t.Errorf("StdlibPackages(nil, nil) = %#v, want empty slice", got)
	}
}

func TestStdlibPackages(t *testing.T) {
	const (
		module = "/app"
		goroot = "/goroot"
	)
	raw := `
Samples:
alloc_objects/count alloc_space/bytes
         10        100: 1
         20        200: 2
          7         70: 3
         11         11: 4
          3         30: 5
          8         80: 6
          4         40: 7
          9         90: 8
          0          0: 1
          5         50: 9
          1          8: 10
Locations
     1: 0x1 M=1 encoding/json.(*encodeState).marshal /goroot/src/encoding/json/encode.go:1:0 s=1
     2: 0x2 M=1 runtime.mallocgc /goroot/src/runtime/malloc.go:2:0 s=1
             fmt.Sprintf /goroot/src/fmt/print.go:2:0 s=1
             encoding/json.Marshal /goroot/src/encoding/json/encode.go:3:0 s=1
     3: 0x3 M=1 bytes.growSlice /goroot/src/bytes/buffer.go:3:0 s=1
     4: 0x4 M=1 bytes.NewReader /goroot/src/bytes/reader.go:4:0 s=1
     5: 0x5 M=1 fmt.TestPrint /goroot/src/fmt/print_test.go:5:0 s=1
             bytes.growSlice /goroot/src/bytes/buffer.go:3:0 s=1
     6: 0x6 M=1 app.miss /app/miss.go:6:0 s=1
             fmt.Sprintf /goroot/src/fmt/print.go:2:0 s=1
     7: 0x7 M=1 mod.F /Users/x/go/pkg/mod/github.com/foo@v1.0.0/a.go:7:0 s=1
     8: 0x8 M=1 runtime.mallocgc /goroot/src/runtime/malloc.go:2:0 s=1
             app.hit /app/hit.go:4:0 s=1
     9: 0x9 M=1 runtime/pprof.(*Profile).WriteTo /goroot/src/runtime/pprof/pprof.go:9:0 s=1
    10: 0xa M=1 runtime.mallocgc /goroot/src/runtime/malloc.go:2:0 s=1
Mappings
`
	samples, err := ParseAllocProfile(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseAllocProfile() error = %v, want nil", err)
	}
	sites := []Site{{Category: "static", Pos: module + "/hit.go:4"}}
	got := StdlibPackages(sites, samples, module, goroot)
	want := []Weight{
		{Category: "fmt", Objects: 20, Bytes: 200},
		{Category: "encoding/json", Objects: 10, Bytes: 100},
		{Category: "bytes", Objects: 18, Bytes: 81},
		{Category: "runtime/pprof", Objects: 5, Bytes: 50},
	}
	if fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", want) {
		t.Errorf("StdlibPackages(profile) = %#v, want %#v", got, want)
	}

	var gotObjects, gotBytes int64
	for _, weight := range got {
		gotObjects += weight.Objects
		gotBytes += weight.Bytes
	}
	var stdlib Weight
	var found bool
	for _, weight := range Outside(sites, samples, module, goroot) {
		if weight.Category != OutsideStdlib {
			continue
		}
		found = true
		stdlib = weight
	}
	if !found {
		t.Fatalf("Outside stdlib missing, StdlibPackages sum = %d objects, %d bytes", gotObjects, gotBytes)
	}
	if gotObjects != stdlib.Objects || gotBytes != stdlib.Bytes {
		t.Errorf("StdlibPackages sum = %d objects, %d bytes, want Outside stdlib %d, %d",
			gotObjects, gotBytes, stdlib.Objects, stdlib.Bytes)
	}
}

func TestStdlibCategoriesEmpty(t *testing.T) {
	got := StdlibCategories(nil, nil, nil, "", "")
	if got == nil || len(got) != 0 {
		t.Errorf("StdlibCategories(nil, nil) = %#v, want empty slice", got)
	}
}

func TestStdlibCategories(t *testing.T) {
	const (
		module = "/app"
		goroot = "/goroot"
	)
	raw := `
Samples:
alloc_objects/count alloc_space/bytes
         10        100: 1
         20        200: 2
          7         70: 3
         11         11: 4
          3         30: 5
          8         80: 6
          4         40: 7
          9         90: 8
          0          0: 1
          5         50: 9
          1          8: 10
Locations
     1: 0x1 M=1 encoding/json.(*encodeState).marshal /goroot/src/encoding/json/encode.go:1:0 s=1
     2: 0x2 M=1 runtime.mallocgc /goroot/src/runtime/malloc.go:2:0 s=1
             fmt.Sprintf /goroot/src/fmt/print.go:2:0 s=1
             encoding/json.Marshal /goroot/src/encoding/json/encode.go:3:0 s=1
     3: 0x3 M=1 bytes.growSlice /goroot/src/bytes/buffer.go:3:0 s=1
     4: 0x4 M=1 bytes.NewReader /goroot/src/bytes/reader.go:4:0 s=1
     5: 0x5 M=1 fmt.TestPrint /goroot/src/fmt/print_test.go:5:0 s=1
             bytes.growSlice /goroot/src/bytes/buffer.go:3:0 s=1
     6: 0x6 M=1 app.miss /app/miss.go:6:0 s=1
             fmt.Sprintf /goroot/src/fmt/print.go:2:0 s=1
     7: 0x7 M=1 mod.F /Users/x/go/pkg/mod/github.com/foo@v1.0.0/a.go:7:0 s=1
     8: 0x8 M=1 runtime.mallocgc /goroot/src/runtime/malloc.go:2:0 s=1
             app.hit /app/hit.go:4:0 s=1
     9: 0x9 M=1 runtime/pprof.(*Profile).WriteTo /goroot/src/runtime/pprof/pprof.go:9:0 s=1
    10: 0xa M=1 runtime.mallocgc /goroot/src/runtime/malloc.go:2:0 s=1
Mappings
`
	samples, err := ParseAllocProfile(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseAllocProfile() error = %v, want nil", err)
	}
	moduleSites := []Site{{Category: "static", Pos: module + "/hit.go:4"}}
	stdlibSites := []Site{
		{Category: "static", Pos: goroot + "/src/fmt/print.go:2"},
		{Category: "rc-local", Pos: goroot + "/src/bytes/buffer.go:3"},
		{Category: "static", Pos: goroot + "/src/bytes/buffer.go:3"},
		{Category: "cyclic", Pos: goroot + "/src/encoding/json/encode.go:3"},
	}
	got := StdlibCategories(moduleSites, stdlibSites, samples, module, goroot)
	want := []Weight{
		{Category: "static", Sites: 2, Objects: 23, Bytes: 235},
		{Category: "rc-local", Sites: 1, Objects: 4, Bytes: 35},
		{Category: "cyclic", Sites: 1},
		{Category: Unmatched, Objects: 26, Bytes: 161},
	}
	if fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", want) {
		t.Errorf("StdlibCategories(profile) = %#v, want %#v", got, want)
	}

	var gotObjects, gotBytes int64
	for _, weight := range got {
		gotObjects += weight.Objects
		gotBytes += weight.Bytes
	}
	var stdlib Weight
	var found bool
	for _, weight := range Outside(moduleSites, samples, module, goroot) {
		if weight.Category != OutsideStdlib {
			continue
		}
		found = true
		stdlib = weight
	}
	if !found {
		t.Fatalf("Outside stdlib missing, StdlibCategories sum = %d objects, %d bytes", gotObjects, gotBytes)
	}
	if gotObjects != stdlib.Objects || gotBytes != stdlib.Bytes {
		t.Errorf("StdlibCategories sum = %d objects, %d bytes, want Outside stdlib %d, %d",
			gotObjects, gotBytes, stdlib.Objects, stdlib.Bytes)
	}
}

func TestExternalModulesEmpty(t *testing.T) {
	got := ExternalModules(nil, nil, "", "")
	if got == nil || len(got) != 0 {
		t.Errorf("ExternalModules(nil, nil) = %#v, want empty slice", got)
	}
}

func TestExternalModules(t *testing.T) {
	const (
		module = "/app"
		goroot = "/goroot"
	)
	raw := `
Samples:
alloc_objects/count alloc_space/bytes
         10        100: 1
          4         40: 2
          3         30: 3
          2         20: 4
          5         50: 5
          6         60: 6
          1         15: 7
          8         80: 8
          9          9: 9
          0          0: 1
         12         12: 10
          7         70: 11
          1         11: 12
Locations
     1: 0x1 M=1 mod.F /Users/x/go/pkg/mod/github.com/foo/bar@v1.2.3/a.go:1:0 s=1
     2: 0x2 M=1 mod.F /Users/x/go/pkg/mod/github.com/foo/bar@v1.2.4/b.go:2:0 s=1
     3: 0x3 M=1 yaml.F /Users/x/go/pkg/mod/gopkg.in/yaml.v3@v3.0.1/yaml.go:3:0 s=1
     4: 0x4 M=1 null.F /Users/x/go/pkg/mod/github.com/guregu/null@v4.0.0+incompatible/null.go:4:0 s=1
     5: 0x5 M=1 local.F /opt/other/local/mod.go:5:0 s=1
     6: 0x6 M=1 plain.F /Users/x/go/pkg/mod/github.com/plain/mod/file.go:6:0 s=1
     7: 0x7 M=1 mock.F /Users/x/go/pkg/mod/github.com/!d!a!t!a-!d!o!g/go-sqlmock@v1.5.2/sqlmock.go:7:0 s=1
     8: 0x8 M=1 runtime.mallocgc /goroot/src/runtime/malloc.go:8:0 s=1
             sys.F /Users/x/go/pkg/mod/golang.org/x/sys@v0.0.0-20240101120000-abcdef012345/unix/sys.go:8:0 s=1
     9: 0x9 M=1 dep.TestF /Users/x/go/pkg/mod/github.com/foo/bar@v1.2.3/a_test.go:9:0 s=1
    10: 0xa M=1 runtime.mallocgc /goroot/src/runtime/malloc.go:8:0 s=1
             app.hit /app/hit.go:4:0 s=1
    11: 0xb M=1 fmt.Sprintf /goroot/src/fmt/print.go:11:0 s=1
    12: 0xc M=1 vend.F /opt/vendor/foo@v1.0.0/a.go:12:0 s=1
Mappings
`
	samples, err := ParseAllocProfile(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseAllocProfile() error = %v, want nil", err)
	}
	sites := []Site{{Category: "static", Pos: module + "/hit.go:4"}}
	got := ExternalModules(sites, samples, module, goroot)
	want := []Weight{
		{Category: "github.com/foo/bar", Objects: 14, Bytes: 140},
		{Category: OutsideUncached, Objects: 12, Bytes: 121},
		{Category: "golang.org/x/sys", Objects: 8, Bytes: 80},
		{Category: "gopkg.in/yaml.v3", Objects: 3, Bytes: 30},
		{Category: "github.com/guregu/null", Objects: 2, Bytes: 20},
		{Category: "github.com/!d!a!t!a-!d!o!g/go-sqlmock", Objects: 1, Bytes: 15},
	}
	if fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", want) {
		t.Errorf("ExternalModules(profile) = %#v, want %#v", got, want)
	}

	var gotObjects, gotBytes int64
	for _, weight := range got {
		gotObjects += weight.Objects
		gotBytes += weight.Bytes
	}
	var external Weight
	var found bool
	for _, weight := range Outside(sites, samples, module, goroot) {
		if weight.Category != OutsideExternal {
			continue
		}
		found = true
		external = weight
	}
	if !found {
		t.Fatalf("Outside external missing, ExternalModules sum = %d objects, %d bytes", gotObjects, gotBytes)
	}
	if gotObjects != external.Objects || gotBytes != external.Bytes {
		t.Errorf("ExternalModules sum = %d objects, %d bytes, want Outside external %d, %d",
			gotObjects, gotBytes, external.Objects, external.Bytes)
	}
}

func TestExternalCategoriesEmpty(t *testing.T) {
	got := ExternalCategories(nil, nil, nil, "", "")
	if got == nil || len(got) != 0 {
		t.Errorf("ExternalCategories(nil, nil) = %#v, want empty slice", got)
	}
	dirs := ExternalPackageDirs(nil, nil, "", "")
	if dirs == nil || len(dirs) != 0 {
		t.Errorf("ExternalPackageDirs(nil, nil) = %#v, want empty slice", dirs)
	}
}

func TestExternalCategories(t *testing.T) {
	const (
		module = "/app"
		goroot = "/goroot"
	)
	root := t.TempDir()
	presentA := filepath.Join(root, "pkg", "mod", "github.com", "foo", "bar@v1.2.3", "a.go")
	presentB := filepath.Join(root, "pkg", "mod", "github.com", "foo", "bar@v1.2.3", "b.go")
	presentC := filepath.Join(root, "pkg", "mod", "gopkg.in", "yaml.v3@v3.0.1", "yaml.go")
	uncached := filepath.Join(root, "opt", "other", "local", "mod.go")
	missing := filepath.Join(root, "pkg", "mod", "github.com", "gone", "mod@v1.0.0", "z.go")
	dirOnly := filepath.Join(root, "pkg", "mod", "github.com", "dir", "mod@v1.0.0", "sub")
	testFile := filepath.Join(root, "pkg", "mod", "github.com", "foo", "bar@v1.2.3", "a_test.go")
	for _, path := range []string{presentA, presentB, presentC, uncached, testFile} {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", path, err)
		}
		if err := os.WriteFile(path, []byte("package p\n"), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", path, err)
		}
	}
	if err := os.MkdirAll(dirOnly, 0o750); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", dirOnly, err)
	}

	raw := fmt.Sprintf(`
Samples:
alloc_objects/count alloc_space/bytes
         10        100: 1
          7         70: 2
         11         11: 3
          4         40: 4
          2         20: 5
          5         50: 6
          6         60: 7
          8         80: 8
          9          9: 9
          0          0: 1
          3         30: 10
          1         15: 11
         12         12: 12
Locations
     1: 0x1 M=1 foo.F %s:1:0 s=1
     2: 0x2 M=1 foo.G %s:2:0 s=1
     3: 0x3 M=1 yaml.F %s:3:0 s=1
     4: 0x4 M=1 gone.F %s:4:0 s=1
     5: 0x5 M=1 dir.F %s:5:0 s=1
     6: 0x6 M=1 runtime.mallocgc %s/src/runtime/malloc.go:6:0 s=1
             foo.F %s:1:0 s=1
     7: 0x7 M=1 foo.TestF %s:7:0 s=1
     8: 0x8 M=1 fmt.Sprintf %s/src/fmt/print.go:8:0 s=1
     9: 0x9 M=1 runtime.mallocgc %s/src/runtime/malloc.go:6:0 s=1
             app.hit %s/hit.go:4:0 s=1
    10: 0xa M=1 gone.F %s:4:0 s=1
             foo.F %s:1:0 s=1
    11: 0xb M=1 local.F %s:6:0 s=1
    12: 0xc M=1 runtime.mallocgc %s/src/runtime/malloc.go:6:0 s=1
Mappings
`, presentA, presentB, presentC, missing, dirOnly, goroot, presentA, testFile, goroot, goroot, module, missing, presentA, uncached, goroot)
	samples, err := ParseAllocProfile(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseAllocProfile() error = %v, want nil", err)
	}
	moduleSites := []Site{{Category: "static", Pos: module + "/hit.go:4"}}
	externalSites := []Site{
		{Category: "static", Pos: presentA + ":1"},
		{Category: "static", Pos: presentB + ":2"},
		{Category: "rc-local", Pos: presentB + ":2"},
		{Category: "cyclic", Pos: presentA + ":9"},
		{Category: "static", Pos: uncached + ":6"},
	}
	got := ExternalCategories(moduleSites, externalSites, samples, module, goroot)
	want := []Weight{
		{Category: "static", Sites: 3, Objects: 20, Bytes: 200},
		{Category: "rc-local", Sites: 1, Objects: 3, Bytes: 35},
		{Category: "cyclic", Sites: 1},
		{Category: Unmatched, Objects: 11, Bytes: 11},
		{Category: OutsideNoSource, Objects: 9, Bytes: 90},
	}
	if fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", want) {
		t.Errorf("ExternalCategories(profile) = %#v, want %#v", got, want)
	}

	var gotObjects, gotBytes int64
	for _, weight := range got {
		gotObjects += weight.Objects
		gotBytes += weight.Bytes
	}
	var external Weight
	var found bool
	for _, weight := range Outside(moduleSites, samples, module, goroot) {
		if weight.Category != OutsideExternal {
			continue
		}
		found = true
		external = weight
	}
	if !found {
		t.Fatalf("Outside external missing, ExternalCategories sum = %d objects, %d bytes", gotObjects, gotBytes)
	}
	if gotObjects != external.Objects || gotBytes != external.Bytes {
		t.Errorf("ExternalCategories sum = %d objects, %d bytes, want Outside external %d, %d",
			gotObjects, gotBytes, external.Objects, external.Bytes)
	}

	dirs := ExternalPackageDirs(moduleSites, samples, module, goroot)
	wantDirs := []string{filepath.Dir(presentA), filepath.Dir(presentC), filepath.Dir(uncached)}
	for i, dir := range wantDirs {
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			t.Fatalf("EvalSymlinks(%q) error = %v", dir, err)
		}
		wantDirs[i] = resolved
	}
	slices.Sort(wantDirs)
	if fmt.Sprintf("%#v", dirs) != fmt.Sprintf("%#v", wantDirs) {
		t.Errorf("ExternalPackageDirs(profile) = %#v, want %#v", dirs, wantDirs)
	}
}
