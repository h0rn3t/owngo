package main

import "testing"

// TestRun запускає програму збіркою gc, зокрема під go test -race.
func TestRun(t *testing.T) {
	if got := run(); got != 7 {
		t.Errorf("run() = %d, want 7", got)
	}
}
