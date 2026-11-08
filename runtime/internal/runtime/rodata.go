package runtime

// rodata and erodata stand for the bounds of the read-only data section that
// gc's linker defines. Libraries take them through go:linkname to tell whether
// a pointer is read-only (github.com/gofiber/fiber/v3 readonly.go). llgo lays
// out no such section, so the range holds at most rodata itself: every
// pointer reads as writable, the caller's answer on s390x and ppc64.
var rodata, erodata byte
