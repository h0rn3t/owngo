//go:build owngo && owngomn && owngoasan && (linux || darwin)

package runtime

import "unsafe"

func bindAllocatorCache(*unsafe.Pointer) {}

func unbindAllocatorCache() {}
