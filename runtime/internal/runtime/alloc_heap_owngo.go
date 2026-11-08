//go:build owngo && !owngoasan && linux && owngostat && owngoheap

package runtime

// З тегами owngostat і owngoheap алокатор з лічильниками ще й семплює купу:
// стек кожного 64-го виділення і його звільнення, дамп у $OWNGO_HEAP_OUT
// на виході (P-NEW-09.3, owngo/bench/heapsym.py).
const owngoAllocCFile = "_wrap/owngo_alloc_heap.c"
