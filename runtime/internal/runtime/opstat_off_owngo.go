//go:build owngo && !owngostat

package runtime

// Без тегу owngostat лічильників операцій немає: порожні тіла компілятор
// вбудовує і прибирає.
func countOp(op int32) {}

func reportOps() {}
