package v1lib

func Keep(*int) {}

var G *int

func Stash(p *int) { G = p }
