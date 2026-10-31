package main

import (
	"os"
	"testing"
)

func TestRun(t *testing.T) {
	want, err := os.ReadFile("testdata/expected-10.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got := run(10); string(got) != string(want) {
		t.Errorf("run(10) = %q, want %q", got, want)
	}
}
