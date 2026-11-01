package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	want, err := os.ReadFile("testdata/expected-200.pbm")
	if err != nil {
		t.Fatal(err)
	}
	// Розмір округлюється вниз до кратного 8, як у Rust #4.
	for _, size := range []int{200, 203} {
		if got := run(size); !bytes.Equal(got, want) {
			t.Errorf("run(%d) differs from expected-200.pbm: %d bytes, want %d", size, len(got), len(want))
		}
	}
}

func TestRun16000(t *testing.T) {
	if testing.Short() {
		t.Skip("16000×16000 takes seconds")
	}
	want, err := os.ReadFile("testdata/expected-16000.sha256")
	if err != nil {
		t.Fatal(err)
	}
	got := fmt.Sprintf("%x", sha256.Sum256(run(16000)))
	if got != strings.TrimSpace(string(want)) {
		t.Errorf("sha256(run(16000)) = %s, want %s", got, want)
	}
}

func TestRunEmpty(t *testing.T) {
	for _, size := range []int{0, 7} {
		if got, want := string(run(size)), "P4\n0 0\n"; got != want {
			t.Errorf("run(%d) = %q, want %q", size, got, want)
		}
	}
}
