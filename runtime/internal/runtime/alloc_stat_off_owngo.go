//go:build owngo && !owngoasan && (linux || darwin) && !owngostat

package runtime

const owngoAllocCFile = "_wrap/owngo_alloc.c"
