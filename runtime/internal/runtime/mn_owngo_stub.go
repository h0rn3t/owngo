//go:build !owngo || !owngomn || !(linux || darwin)

package runtime

const owngoMNLLGoFiles = ""

// localContextMigrates: горутина завершується на тому самому потоці, де
// почалась, і вкладений вихід перевіряє свій контекст.
const localContextMigrates = false
