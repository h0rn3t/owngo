//go:build !owngo || !owngomn || !(linux || darwin)

package runtime

func procShard() (uint32, bool) { return 0, false }
