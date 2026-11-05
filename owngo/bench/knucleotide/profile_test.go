package main

import (
	"os"
	"testing"
)

// TestProfile кличе main на вході fasta 1000 (testdata/input-1000.txt, файл
// Benchmarks Game): профіль алокацій для census знімає owngo/bench/census.sh.
func TestProfile(t *testing.T) {
	in, err := os.Open("testdata/input-1000.txt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { in.Close() })
	out, err := os.Create(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { out.Close() })
	args, stdin, stdout := os.Args, os.Stdin, os.Stdout
	t.Cleanup(func() { os.Args, os.Stdin, os.Stdout = args, stdin, stdout })
	os.Args, os.Stdin, os.Stdout = []string{"knucleotide", "0"}, in, out
	main()
}
