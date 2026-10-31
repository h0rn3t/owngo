package main

import (
	"os"
	"testing"
)

// TestProfile кличе main з аргументом 7: профіль алокацій для census
// знімає owngo/bench/census.sh.
func TestProfile(t *testing.T) {
	args := os.Args
	t.Cleanup(func() { os.Args = args })
	os.Args = []string{"fannkuchredux", "7"}
	main()
}
