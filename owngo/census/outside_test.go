package census

import (
	"fmt"
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
