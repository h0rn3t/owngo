//go:build owngo && owngoasan

package runtime

// Під ASan кожен об'єкт регіону — окремий malloc, і FreeRegion звільняє
// кожен: читання після FreeRegion ASan бачить як heap-use-after-free.
const regionPerObject = true
