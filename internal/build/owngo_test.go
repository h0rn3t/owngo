package build

import (
	"runtime"
	"testing"
)

func TestApplyOwngoTag(t *testing.T) {
	tests := []struct {
		name string
		tags string
		asan bool
		want string
	}{
		{name: "empty", tags: "", want: ""},
		{name: "other", tags: "purego", want: "purego"},
		{name: "owngo", tags: "owngo", want: "owngo,nogc"},
		{name: "owngo among others", tags: "foo,owngo,bar", want: "foo,owngo,bar,nogc"},
		{name: "already nogc", tags: "owngo,nogc", want: "owngo,nogc"},
		{name: "nogc before owngo", tags: "nogc,owngo", want: "nogc,owngo"},
		{name: "owngo asan", tags: "owngo", asan: true, want: "owngo,nogc,owngoasan"},
		{name: "asan without owngo", tags: "purego", asan: true, want: "purego"},
		{name: "owngoasan given", tags: "owngo,nogc,owngoasan", asan: true, want: "owngo,nogc,owngoasan"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			conf := &Config{Tags: test.tags, ASan: test.asan}
			applyOwngoTag(conf)
			if conf.Tags != test.want {
				t.Errorf("applyOwngoTag(%q, asan %v) = %q, want %q", test.tags, test.asan, conf.Tags, test.want)
			}
		})
	}
}

func TestASanRequiresNativeHost(t *testing.T) {
	conf, err := resolveBuildConfig(&Config{ASan: true})
	if err != nil {
		t.Fatalf("resolveBuildConfig(host asan) error = %v", err)
	}
	if !conf.ASan {
		t.Fatal("resolveBuildConfig(host asan) ASan = false")
	}
	if _, err := resolveBuildConfig(&Config{ASan: true, Target: "wasi"}); err == nil {
		t.Fatal("resolveBuildConfig(asan, wasi) error = nil")
	}
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		return
	}
	if _, err := resolveBuildConfig(&Config{ASan: true, Goos: "linux", Goarch: "amd64"}); err == nil {
		t.Fatal("resolveBuildConfig(asan cross) error = nil")
	}
}
