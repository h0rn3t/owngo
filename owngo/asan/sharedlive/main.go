// Програма прийняття P9.5: горутина читає захоплену змінну вже після
// того, як функція, що її створила, повернулась і поставила свій
// DecShared. Посилання горутини ще живе, тож ASan звіту не дає.
package main

import "os"

func spawn(start <-chan struct{}, done chan<- int) {
	var x [4]int
	x[0] = 7
	go func() {
		<-start
		done <- x[0]
	}()
}

func run() int {
	start := make(chan struct{})
	done := make(chan int)
	spawn(start, done)
	close(start)
	return <-done
}

func main() {
	if run() != 7 {
		os.Exit(2)
	}
	println("asan-sharedlive-ok")
}
