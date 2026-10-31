package census

import (
	"fmt"
	"strings"
	"testing"
)

func TestUnmatchedReasonsEmpty(t *testing.T) {
	got := UnmatchedReasons(nil, nil, nil, "", "")
	if got.Reasons == nil || len(got.Reasons) != 0 || got.Stdlib == nil || len(got.Stdlib) != 0 ||
		got.Lines == nil || len(got.Lines) != 0 || len(got.Packages) != 0 {
		t.Errorf("UnmatchedReasons(nil) = %#v, want empty slices", got)
	}
}

func TestUnmatchedReasons(t *testing.T) {
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
         60        600: 6
         70        700: 7
         80        800: 8
          5         50: 9
          1          8: 10
          2         16: 11
Locations
     1: 0x1 M=1 testing.tRunner /goroot/src/testing/testing.go:10:0 s=1
     2: 0x2 M=1 flag.Bool /goroot/src/flag/flag.go:20:0 s=1
             testing.Init /goroot/src/testing/testing.go:21:0 s=1
     3: 0x3 M=1 app.TestProfile /app/profile_test.go:30:0 s=1
     4: 0x4 M=1 runtime.malg /goroot/src/runtime/proc.go:40:0 s=1
     5: 0x5 M=1 runtime.mallocgc /goroot/src/runtime/malloc.go:4:0 s=1
             fmt.Sprintf /goroot/src/fmt/print.go:50:0 s=1
             main.main /app/main.go:51:0 s=1
     6: 0x6 M=1 strconv.Itoa /goroot/src/strconv/itoa.go:60:0 s=1
             fmt.Sprintf /goroot/src/fmt/print.go:61:0 s=1
             main.run /app/main.go:62:0 s=1
     7: 0x7 M=1 mod.F /x/go/pkg/mod/github.com/foo@v1.0.0/a.go:70:0 s=1
             main.main /app/main.go:71:0 s=1
     8: 0x8 M=1 runtime.newobject /goroot/src/runtime/malloc.go:80:0 s=1
             main.run /app/main.go:81:0 s=1
             testing.tRunner /goroot/src/testing/testing.go:82:0 s=1
     9: 0x9 M=1 main.hit /app/main.go:90:0 s=1
    10: 0xa M=1 main.run /app/main.go:81:0 s=1
    11: 0xb M=1 main.leak /app/main.go:95:0 s=1
Mappings
`
	samples, err := ParseAllocProfile(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseAllocProfile() error = %v, want nil", err)
	}
	sites := []Site{{Category: "static", Pos: module + "/main.go:90"}}
	stdlibSites := []Site{{Category: "static", Pos: goroot + "/src/fmt/print.go:50"}}

	got := UnmatchedReasons(sites, stdlibSites, samples, module, goroot)
	want := UnmatchedSplit{
		// Байти testing, і stdlib, який кличе лише testing, — обв'язка;
		// кадр програми в стеку робить вибірку рядком модуля.
		Reasons: []Weight{
			{Category: OutsideHarness, Objects: 60, Bytes: 600},
			{Category: OutsideRuntime, Objects: 40, Bytes: 400},
			{Category: OutsideStdlib, Objects: 110, Bytes: 1100},
			{Category: OutsideExternal, Objects: 70, Bytes: 700},
			{Category: OutsideModule, Objects: 83, Bytes: 824},
		},
		Stdlib: []Weight{
			{Category: "static", Sites: 1, Objects: 50, Bytes: 500},
			{Category: Unmatched, Objects: 60, Bytes: 600},
		},
		Packages: []string{"fmt", "strconv"},
		Lines: []LineWeight{
			{Func: "main.run", Pos: module + "/main.go:81", Objects: 81, Bytes: 808},
			{Func: "main.leak", Pos: module + "/main.go:95", Objects: 2, Bytes: 16},
		},
	}
	if fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", want) {
		t.Errorf("UnmatchedReasons(profile) =\n%#v\nwant\n%#v", got, want)
	}

	var reasonObjects, reasonBytes int64
	for _, reason := range got.Reasons {
		reasonObjects += reason.Objects
		reasonBytes += reason.Bytes
	}
	weights := Weigh(sites, samples)
	last := weights[len(weights)-1]
	if last.Category != Unmatched || last.Objects != reasonObjects || last.Bytes != reasonBytes {
		t.Errorf("UnmatchedReasons sum = %d objects, %d bytes, want Weigh %#v", reasonObjects, reasonBytes, last)
	}
}
