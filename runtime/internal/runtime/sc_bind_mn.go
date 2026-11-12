//go:build owngo && owngomn && !owngoasan && (linux || darwin)

package runtime

import "unsafe"

//go:linkname owngoSCAttach C.owngo_sc_attach
func owngoSCAttach(held *unsafe.Pointer)

//go:linkname owngoSCDetach C.owngo_sc_detach
func owngoSCDetach()

func bindAllocatorCache(held *unsafe.Pointer) { owngoSCAttach(held) }

func unbindAllocatorCache() { owngoSCDetach() }
