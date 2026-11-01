// Програма прийняття P9.5: вісім горутин захоплюють одну змінну і читають
// її після DecShared у функції, що її створила. Останнє посилання віддає
// одна з горутин, тож ASan звіту не дає.
package main

import "os"

func spawn(start <-chan struct{}, done chan<- int) {
	var x [4]int
	x[0] = 7
	for i := range 8 {
		go func() {
			<-start
			done <- x[0] + i
		}()
	}
}

func run() int {
	start := make(chan struct{})
	done := make(chan int)
	spawn(start, done)
	close(start)
	sum := 0
	for range 8 {
		sum += <-done
	}
	return sum
}

func main() {
	// 8·7 + 0+1+…+7.
	if run() != 84 {
		os.Exit(2)
	}
	println("asan-sharedeight-ok")
}
