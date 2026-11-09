//go:build !(owngo && owngomn && (linux || darwin))

package runtime

func gomaxprocs(int) int { return int(c_maxprocs()) }
