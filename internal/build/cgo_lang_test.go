package build

import (
	"reflect"
	"slices"
	"testing"
)

func TestLLGoFileCompilerLanguage(t *testing.T) {
	ctx := &context{buildConf: &Config{}}
	for _, tc := range []struct {
		name string
		file string
		args []string
		want []string
	}{
		{name: "C default", file: "p.c", want: []string{"-x", "c"}},
		{name: "C++ extension", file: "p.cpp"},
		{name: "uppercase C is C++", file: "p.C"},
		{name: "joined language", file: "p.c", args: []string{"-xc++"}, want: []string{"-x", "c", "-xc++"}},
		{name: "reset language", file: "p.c", args: []string{"-x", "none"}, want: []string{"-x", "c", "-x", "none"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := slices.Clone(tc.args)
			got := llgoFileCompilerArgs(ctx, tc.args, tc.file)
			want := append(slices.Clone(tc.want), debugInfoCompilerArgs(ctx.buildConf, &ctx.crossCompile)...)
			if !slices.Equal(got, want) {
				t.Fatalf("compiler arguments = %q, want %q", got, want)
			}
			if !reflect.DeepEqual(tc.args, before) {
				t.Fatal("compiler arguments modified the caller's slice")
			}
		})
	}
}
