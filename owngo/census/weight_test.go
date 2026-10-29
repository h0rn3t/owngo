package census

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestWeighEmpty(t *testing.T) {
	got := Weigh(nil, nil)
	if got == nil || len(got) != 0 {
		t.Errorf("Weigh(nil, nil) = %#v, want empty slice", got)
	}
}

func TestWeigh(t *testing.T) {
	tests := []struct {
		name    string
		sites   []Site
		samples []AllocSample
		want    []Weight
	}{
		{
			name: "site counts keep cascade order",
			sites: []Site{
				{Category: "rc-local", Pos: "/app/b.go:2"},
				{Category: "static", Pos: "/app/a.go:1"},
				{Category: "static", Pos: "/app/c.go:3"},
				{Category: "stack", Pos: "/app/s.go:4"},
			},
			want: []Weight{
				{Category: "stack", Sites: 1},
				{Category: "static", Sites: 2},
				{Category: "rc-local", Sites: 1},
			},
		},
		{
			name: "leaf frame wins over a later matching caller",
			sites: []Site{
				{Category: "static", Pos: "/app/b.go:2"},
				{Category: "rc-local", Pos: "/app/a.go:1"},
			},
			samples: []AllocSample{{
				Objects: 7,
				Bytes:   70,
				Frames: []Frame{
					{File: "/app/a.go", Line: 1},
					{File: "/app/b.go", Line: 2},
				},
			}},
			want: []Weight{
				{Category: "static", Sites: 1},
				{Category: "rc-local", Sites: 1, Objects: 7, Bytes: 70},
			},
		},
		{
			name: "runtime leaf falls through to the user frame",
			sites: []Site{
				{Category: "static", Pos: "/app/a.go:1"},
			},
			samples: []AllocSample{{
				Objects: 7,
				Bytes:   70,
				Frames: []Frame{
					{File: "/goroot/src/runtime/malloc.go", Line: 10},
					{File: "/app/a.go", Line: 1},
				},
			}},
			want: []Weight{
				{Category: "static", Sites: 1, Objects: 7, Bytes: 70},
			},
		},
		{
			name: "same line splits remainder toward earlier sites",
			sites: []Site{
				{Category: "rc-local", Pos: "/app/a.go:5"},
				{Category: "implicit", Pos: "/app/a.go:5"},
			},
			samples: []AllocSample{{
				Objects: 5,
				Bytes:   101,
				Frames:  []Frame{{File: "/app/a.go", Line: 5}},
			}},
			want: []Weight{
				{Category: "rc-local", Sites: 1, Objects: 3, Bytes: 51},
				{Category: "implicit", Sites: 1, Objects: 2, Bytes: 50},
			},
		},
		{
			name: "one object goes to the earlier site",
			sites: []Site{
				{Category: "rc-local", Pos: "/app/a.go:5"},
				{Category: "implicit", Pos: "/app/a.go:5"},
			},
			samples: []AllocSample{{
				Objects: 1,
				Bytes:   1,
				Frames:  []Frame{{File: "/app/a.go", Line: 5}},
			}},
			want: []Weight{
				{Category: "rc-local", Sites: 1, Objects: 1, Bytes: 1},
				{Category: "implicit", Sites: 1},
			},
		},
		{
			name: "same category on one line keeps the whole sample",
			sites: []Site{
				{Category: "implicit", Pos: "/app/a.go:5"},
				{Category: "implicit", Pos: "/app/a.go:5"},
			},
			samples: []AllocSample{{
				Objects: 5,
				Bytes:   40,
				Frames:  []Frame{{File: "/app/a.go", Line: 5}},
			}},
			want: []Weight{
				{Category: "implicit", Sites: 2, Objects: 5, Bytes: 40},
			},
		},
		{
			name: "samples accumulate and a basename miss is unmatched",
			sites: []Site{
				{Category: "static", Pos: "a.go:4"},
			},
			samples: []AllocSample{
				{Objects: 2, Bytes: 8, Frames: []Frame{{File: "/mod/pkg/a.go", Line: 4}}},
				{Objects: 3, Bytes: 8, Frames: []Frame{{File: "/mod/pkg/a.go", Line: 4}}},
				{Objects: 9, Bytes: 9, Frames: []Frame{{File: "/mod/pkg/ba.go", Line: 4}}},
			},
			want: []Weight{
				{Category: "static", Sites: 1, Objects: 5, Bytes: 16},
				{Category: Unmatched, Objects: 9, Bytes: 9},
			},
		},
		{
			name: "unreadable position still counts as a site",
			sites: []Site{
				{Category: "static", Pos: "noline"},
			},
			want: []Weight{
				{Category: "static", Sites: 1},
			},
		},
		{
			name: "categories outside the cascade sort by name before unmatched",
			sites: []Site{
				{Category: "zebra", Pos: "/app/z.go:1"},
				{Category: "implicit", Pos: "/app/i.go:2"},
				{Category: "alpha", Pos: "/app/a.go:3"},
			},
			samples: []AllocSample{{
				Objects: 1,
				Bytes:   1,
				Frames:  []Frame{{File: "/runtime/malloc.go", Line: 1}},
			}},
			want: []Weight{
				{Category: "implicit", Sites: 1},
				{Category: "alpha", Sites: 1},
				{Category: "zebra", Sites: 1},
				{Category: Unmatched, Objects: 1, Bytes: 1},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Weigh(tt.sites, tt.samples)
			if !slices.Equal(got, tt.want) {
				t.Errorf("Weigh(%s) = %+v, want %+v", tt.name, got, tt.want)
			}
		})
	}
}

func TestFormatWeights(t *testing.T) {
	got := FormatWeights([]Weight{
		{Category: "static", Sites: 1, Objects: 1, Bytes: 1},
		{Category: "rc-local", Sites: 1, Objects: 2, Bytes: 2},
		{Category: Unmatched, Objects: 3, Bytes: 3},
	})
	want := "" +
		"| Категорія | Місця | % місць | Об'єкти | % об'єктів | Байти | % байтів |\n" +
		"| --- | ---: | ---: | ---: | ---: | ---: | ---: |\n" +
		"| static | 1 | 50.0 | 1 | 33.3 | 1 | 33.3 |\n" +
		"| rc-local | 1 | 50.0 | 2 | 66.7 | 2 | 66.7 |\n" +
		"\n" +
		"Поза census: об'єктів 3 (50.0% профілю), байтів 3 (50.0% профілю).\n"
	if got != want {
		t.Errorf("FormatWeights() = %q, want %q", got, want)
	}
}

func TestFormatWeightsEmpty(t *testing.T) {
	got := FormatWeights(nil)
	want := "" +
		"| Категорія | Місця | % місць | Об'єкти | % об'єктів | Байти | % байтів |\n" +
		"| --- | ---: | ---: | ---: | ---: | ---: | ---: |\n"
	if got != want {
		t.Errorf("FormatWeights(nil) = %q, want %q", got, want)
	}
}

const rawAllocProfile = `
PeriodType: space bytes
Period: 1
Samples:
alloc_objects/count alloc_space/bytes[dflt] inuse_objects/count inuse_space/bytes
          0          0          1          8: 1
                bytes:[8]
          2         64          0          0: 2 3
          5        101          1         16: 4
Locations
     1: 0x10 M=1 runtime.mallocgc /goroot/src/runtime/malloc.go:10:0 s=1
     2: 0x20 M=1 main.leaf /app/leaf.go:3:0 s=1
             main.outer /app/outer.go:9:0 s=8
     3: 0x30 M=1 runtime/pprof.(*Profile).WriteTo /goroot/src/runtime/pprof/pprof.go:405:0 s=400
     4: 0x40 M=1 main.only /app/only.go:4:0 s=1
Mappings
1: 0x0/0x1/0x0 /bin [FN]
`

func TestParseAllocProfile(t *testing.T) {
	got, err := ParseAllocProfile(strings.NewReader(rawAllocProfile))
	if err != nil {
		t.Fatalf("ParseAllocProfile() error = %v, want nil", err)
	}
	want := []AllocSample{
		{
			Objects: 2,
			Bytes:   64,
			Frames: []Frame{
				{File: "/app/leaf.go", Line: 3},
				{File: "/app/outer.go", Line: 9},
				{File: "/goroot/src/runtime/pprof/pprof.go", Line: 405},
			},
		},
		{
			Objects: 5,
			Bytes:   101,
			Frames:  []Frame{{File: "/app/only.go", Line: 4}},
		},
	}
	if fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", want) {
		t.Errorf("ParseAllocProfile() = %#v, want %#v", got, want)
	}
}

func TestParseAllocProfileFullName(t *testing.T) {
	// pprof дописує повне ім'я у дужках одразу після s=рядок, без пробілу.
	// Усередині є власні дужки узагальненого типу.
	raw := `
Samples:
alloc_objects/count alloc_space/bytes
          1         32: 1
Locations
     1: 0x20 M=1 maps.Clone[go.shape.func ] /goroot/src/maps/maps.go:55:0 s=50(maps.Clone[go.shape.func(int) (int, error)])
Mappings
`
	got, err := ParseAllocProfile(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseAllocProfile() error = %v, want nil", err)
	}
	want := []AllocSample{{
		Objects: 1,
		Bytes:   32,
		Frames:  []Frame{{File: "/goroot/src/maps/maps.go", Line: 55}},
	}}
	if fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", want) {
		t.Errorf("ParseAllocProfile() = %#v, want %#v", got, want)
	}
}

func TestParseAllocProfileColumnOrder(t *testing.T) {
	raw := `
Samples:
inuse_space/bytes alloc_objects/count alloc_space/bytes
          7          4         80: 1
Locations
     1: 0x1 M=1 main.f /app/a.go:1:0 s=1
Mappings
`
	got, err := ParseAllocProfile(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseAllocProfile() error = %v, want nil", err)
	}
	want := []AllocSample{{
		Objects: 4,
		Bytes:   80,
		Frames:  []Frame{{File: "/app/a.go", Line: 1}},
	}}
	if fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", want) {
		t.Errorf("ParseAllocProfile() = %#v, want %#v", got, want)
	}
}

func TestParseAllocProfileZeros(t *testing.T) {
	raw := `
Samples:
alloc_objects/count alloc_space/bytes
          0          0: 1
Locations
     1: 0x1 M=1 main.f /app/a.go:1:0 s=1
Mappings
`
	got, err := ParseAllocProfile(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseAllocProfile() error = %v, want nil", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("ParseAllocProfile(zeros) = %#v, want empty slice", got)
	}
}

func TestParseAllocProfileErrors(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want error
	}{
		{name: "empty", raw: "", want: ErrAllocProfile},
		{
			name: "no alloc column",
			raw:  "Samples:\nobjects/count\n          1: 1\n",
			want: ErrAllocProfile,
		},
		{
			name: "unknown location",
			raw: `
Samples:
alloc_objects/count alloc_space/bytes
          1          8: 7
Locations
     1: 0x1 M=1 main.f /app/a.go:1:0 s=1
Mappings
`,
			want: ErrProfileLocation,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseAllocProfile(strings.NewReader(tt.raw))
			if !errors.Is(err, tt.want) {
				t.Errorf("ParseAllocProfile() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func FuzzParseAllocProfile(f *testing.F) {
	f.Add("")
	f.Add(rawAllocProfile)
	f.Fuzz(func(t *testing.T, raw string) {
		samples, err := ParseAllocProfile(strings.NewReader(raw))
		if err != nil {
			return
		}
		if samples == nil {
			t.Fatalf("ParseAllocProfile() = nil, nil, want samples")
		}
		for _, sample := range samples {
			if sample.Objects == 0 && sample.Bytes == 0 {
				t.Fatalf("ParseAllocProfile() kept a zero sample %+v", sample)
			}
		}
	})
}

func TestParseAllocProfileFromTool(t *testing.T) {
	dir := t.TempDir()
	src := `package main

import (
	"os"
	"runtime"
	"runtime/pprof"
)

var sink []byte

func main() {
	runtime.MemProfileRate = 1
	sink = make([]byte, 32)
	f, err := os.Create(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer f.Close()
	runtime.GC()
	if err := pprof.Lookup("allocs").WriteTo(f, 0); err != nil {
		panic(err)
	}
}
`
	makeLine := strings.Count(src[:strings.Index(src, "make([]byte, 32)")], "\n") + 1
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/alloc\n\ngo 1.27.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prof := filepath.Join(dir, "alloc.out")
	run := exec.CommandContext(t.Context(), "go", "run", ".", prof) //nolint:gosec // G204: go run тимчасового модуля, який щойно записав цей тест
	run.Dir = dir
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	rawCmd := exec.CommandContext(t.Context(), "go", "tool", "pprof", "-raw", prof) //nolint:gosec // G204: pprof читає профіль цього тесту
	raw, err := rawCmd.Output()
	if err != nil {
		t.Fatalf("go tool pprof -raw: %v", err)
	}
	samples, err := ParseAllocProfile(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseAllocProfile() error = %v, want nil", err)
	}
	var objects, space int64
	for _, sample := range samples {
		for _, frame := range sample.Frames {
			if strings.HasSuffix(frame.File, string(filepath.Separator)+"main.go") && frame.Line == makeLine {
				objects += sample.Objects
				space += sample.Bytes
				break
			}
		}
	}
	if objects < 1 || space < 32 {
		t.Errorf("main.go:%d alloc_objects = %d, alloc_space = %d, want >= 1 and >= 32", makeLine, objects, space)
	}
}
