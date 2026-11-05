package main

import (
	"os"
	"testing"
)

// TestProfile кличе main з аргументом 1000. fasta — генератор входу корпусу
// (compare.py), не програма заміру; тест потрібен census.sh, який обходить
// кожну Go-програму owngo/bench.
func TestProfile(t *testing.T) {
	out, err := os.Create(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { out.Close() })
	args, stdout := os.Args, os.Stdout
	t.Cleanup(func() { os.Args, os.Stdout = args, stdout })
	os.Args, os.Stdout = []string{"fasta", "1000"}, out
	main()
}
