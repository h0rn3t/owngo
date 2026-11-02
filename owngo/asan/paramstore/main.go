//go:build owngo

// Фікстура P11.3: об'єкт, який виклик записав у пам'ять іншого аргументу чи в
// глобальну змінну, живий після виклику (GOROOT/test, internal/godebug.lookup
// і sync.Map.LoadOrStore). Під ASan процес має завершитись без звіту.
package main

import (
	"os"
	"sync"
)

type setting struct{ name string }
type holder struct{ s *setting }
type entry struct{ key, value any }
type table struct{ m []entry }

var (
	g     holder
	tab   table
	cache sync.Map
)

//go:noinline
func direct(h *holder, s *setting) { h.s = s }

//go:noinline
func put(t *table, key, value any) { t.m = append(t.m, entry{key: key, value: value}) }

func lookup(name string) *setting {
	if v, ok := cache.Load(name); ok {
		return v.(*setting)
	}
	s := new(setting)
	s.name = name
	if v, loaded := cache.LoadOrStore(name, s); loaded {
		return v.(*setting)
	}
	return s
}

func main() {
	s := new(setting)
	s.name = "a"
	direct(&g, s)
	t := new(setting)
	t.name = "b"
	put(&tab, "b", t)
	if g.s.name != "a" || tab.m[0].value.(*setting).name != "b" {
		os.Exit(1)
	}
	if lookup("c") != lookup("c") || lookup("c").name != "c" {
		os.Exit(2)
	}
}
