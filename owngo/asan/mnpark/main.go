//go:build owngo && owngomn

// Програма прийняття P-NEW-09.6: G, що паркується, і Ready з іншої M.
// Десять пар горутин передають 10⁵ значень небуферизованим каналом, тож
// паркування і пробудження йдуть впереміш на кількох M. Під -O0 -asan код 0
// лише без звіту: після того як g0 опублікує Gwaiting, інша M може вже
// виконати G, а та звільнить своє очікування, тож g0 не сміє його читати.
package main

type T struct{ a, b, c int }

const pairs, n = 10, 100_000

func main() {
	c := make(chan any)
	done := make(chan bool)
	for range pairs {
		go func() {
			for range n {
				c <- new(T)
			}
			done <- true
		}()
		go func() {
			for range n {
				_ = (<-c).(*T)
			}
			done <- true
		}()
	}
	for range 2 * pairs {
		<-done
	}
}
