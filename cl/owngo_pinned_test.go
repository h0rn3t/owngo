package cl

import (
	"strings"
	"testing"
)

// TestOwnGoPinnedWarning — P10.2: місце pinned пакета, якому збірка дала
// OwnGoWarn, дає один рядок попередження з позицією місця. Пакет без unsafe
// і збірка без тегу owngo рядків не дають.
func TestOwnGoPinnedWarning(t *testing.T) {
	const pinned = `package foo

import "unsafe"

type T struct{ a, b int }

var keep unsafe.Pointer

func Pin() {
	keep = unsafe.Pointer(&T{})
}
`
	const plain = `package foo

type T struct{ a, b int }

var keep *T

func Keep() {
	keep = &T{}
}
`
	// Позиція місця — Lbrace літерала, як у діагностиках census.
	const line = "foo.go:10:26: owngo: pinned: allocation converted to unsafe.Pointer stays live until exit\n"
	tests := []struct {
		name string
		src  string
		free bool
		want string
	}{
		{name: "pinned", src: pinned, free: true, want: line},
		{name: "plain", src: plain, free: true, want: ""},
		{name: "notag", src: pinned, free: false, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ssaPkg, _, files := buildGoSSAPkg(t, tt.src)
			prog := newLLSSAProg(t)
			t.Cleanup(prog.Dispose)
			var warn strings.Builder
			_, _, err := NewPackageExWithEmbedMetaOptions(
				prog, nil, nil, nil, ssaPkg, files, nil, false,
				Options{OwnGoFree: tt.free, OwnGoWarn: &warn},
			)
			if err != nil {
				t.Fatal(err)
			}
			if got := warn.String(); got != tt.want {
				t.Errorf("OwnGoWarn output = %q, want %q", got, tt.want)
			}
		})
	}
}
