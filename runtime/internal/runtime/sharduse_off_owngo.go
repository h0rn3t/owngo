//go:build owngo && !owngostat

package runtime

// Без тегу owngostat облік задіяних шардів не ведеться: порожнє тіло
// компілятор вбудовує і прибирає.
func noteShard(uint32) {}
