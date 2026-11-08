//go:build owngo && !owngoasan && (linux || darwin) && owngostat && !(owngoheap && linux)

package runtime

// З тегом owngostat алокатор збирається з лічильниками (P-NEW-08.1).
// Семплер купи owngoheap — лише Linux (/proc/self/maps), на macOS тег
// нічого не додає.
const owngoAllocCFile = "_wrap/owngo_alloc_stat.c"
