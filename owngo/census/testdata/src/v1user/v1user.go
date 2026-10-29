package v1user

import "v1lib"

func callLibKeep() {
	p := new(int) // want "^static$"
	v1lib.Keep(p)
}

func callLibStash() {
	p := new(int) // want "^rc-shared: move$"
	v1lib.Stash(p)
}
