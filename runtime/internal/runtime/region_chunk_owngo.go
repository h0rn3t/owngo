//go:build owngo && !owngoasan

package runtime

// Об'єкти регіону лягають у шматки.
const regionPerObject = false
