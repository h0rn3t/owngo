//go:build owngo && owngomn

// Програма прийняття P-NEW-09.6: обхід кадрів на стеку горутини з резерву.
// Після паніки з розіменування nil runtime.Caller і debug.Stack мають знайти
// кадр, де вона сталася, і в main, і в горутині, запущеній go: кадри паніки
// знімає обробник сигналу, і межі стека для нього — межі стека G, а не
// потоку. Хвіст стека — як у моделі 1:1: у main — runtime.main і
// runtime.goexit, у горутині — її власна функція або, як у gc,
// runtime.goexit (коли обгортка go має запис кадру, як під -O0), без
// інших кадрів рантайму і C.
// Код 0 лише якщо обидва шляхи знайшли main.fault на рядку 26 і хвости
// збіглися.
package main

import (
	"os"
	"runtime"
	"runtime/debug"
	"strings"
)

var p *int

//go:noinline
func fault() int {
	return *p // рядок 26
}

// tail — імена двох останніх кадрів стека викликача.
//
//go:noinline
func tail() (string, string) {
	pcs := make([]uintptr, 64)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(1, pcs)])
	var prev, last string
	for {
		f, more := frames.Next()
		prev, last = last, f.Function
		if !more {
			return prev, last
		}
	}
}

//go:noinline
func check(who string) (ok bool) {
	defer func() {
		recover()
		ok = false
		for i := 0; ; i++ {
			pc, file, line, more := runtime.Caller(i)
			if !more {
				println(who, "runtime.Caller: no main.fault frame")
				break
			}
			if runtime.FuncForPC(pc).Name() == "main.fault" && strings.HasSuffix(file, "main.go") {
				ok = line == 26
				if !ok {
					println(who, "runtime.Caller: main.fault at line", line)
				}
				break
			}
		}
		if stk := string(debug.Stack()); !strings.Contains(stk, "main.go:26") {
			println(who, "debug.Stack: no main.go:26")
			ok = false
		}
	}()
	fault()
	return true
}

func main() {
	ok := check("main")
	if prev, last := tail(); prev != "runtime.main" || last != "runtime.goexit" {
		println("main: stack ends with", prev, last)
		ok = false
	}
	done := make(chan bool)
	go func() {
		good := check("goroutine")
		if _, last := tail(); !strings.HasPrefix(last, "main.") && last != "runtime.goexit" {
			println("goroutine: stack ends with", last)
			good = false
		}
		done <- good
	}()
	if !<-done || !ok {
		os.Exit(1)
	}
}
