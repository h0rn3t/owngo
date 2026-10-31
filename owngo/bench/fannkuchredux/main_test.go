package main

import (
	"fmt"
	"os"
	"strconv"
	"testing"
)

func TestRun(t *testing.T) {
	for _, n := range []int{7, 12} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			want, err := os.ReadFile(fmt.Sprintf("testdata/expected-%d.txt", n))
			if err != nil {
				t.Fatal(err)
			}
			if got := run(n); string(got) != string(want) {
				t.Errorf("run(%d) = %q, want %q", n, got, want)
			}
		})
	}
}

// Тривіальні випадки Rust #6 повертає без обчислень.
func TestRunTrivial(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0\nPfannkuchen(0) = 0\n"},
		{1, "0\nPfannkuchen(1) = 0\n"},
		{2, "-1\nPfannkuchen(2) = 1\n"},
	}
	for _, tt := range tests {
		if got := run(tt.n); string(got) != tt.want {
			t.Errorf("run(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}
