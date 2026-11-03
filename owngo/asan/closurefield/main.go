//go:build owngo

// Фікстура P11.3: замикання захоплює параметр і через append потрапляє в поле
// структури-параметра; комірка параметра живе, доки живе структура
// (os/exec.(*Cmd).writerDescriptor, 15 випадків GOROOT/test). Під ASan процес
// має завершитись без звіту.
package main

import "os"

type cmd struct {
	jobs []func() int
}

//go:noinline
func (c *cmd) add(w []int) {
	c.jobs = append(c.jobs, func() int {
		return len(w)
	})
}

func main() {
	c := &cmd{}
	c.add(make([]int, 3))
	c.add(make([]int, 4))
	n := 0
	for _, job := range c.jobs {
		n += job()
	}
	if n != 7 {
		os.Exit(1)
	}
}
