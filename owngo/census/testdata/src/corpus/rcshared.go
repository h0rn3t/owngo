package corpus

var g *int

func glob() {
	g = new(int) // want "^rc-shared: global$"
}

var g2 *int

// Перезапис не повертає попереднє значення у static: обидва побували в глобалі.
func overwrite() {
	g2 = new(int) // want "^rc-shared: global$"
	g2 = nil
	g2 = new(int) // want "^rc-shared: global$"
}

var slot struct{ p *int }

func globField() {
	slot.p = new(int) // want "^rc-shared: global$"
}

var gs []int

func globSlice(n int) {
	gs = make([]int, n) // want "^rc-shared: global$"
}

var gm map[string]int

func globMap() {
	gm = make(map[string]int) // want "^rc-shared: global$"
}

func send(ch chan *int) {
	ch <- new(int) // want "^rc-shared: channel$"
}

// Канал не залишає функцію, але надсилання все одно channel.
func localSend() {
	ch := make(chan *int, 1) // want "^static$"
	ch <- new(int)           // want "^rc-shared: channel$"
	p := <-ch
	_ = *p
}

func goroutine() {
	p := new(int) // want "^rc-shared: go$" "^rc-shared: go$"
	go func() {   // want "^rc-shared: go$"
		_ = *p
	}()
}

// go сильніше за call: це не rc-local.
func goArg(use func(*int)) {
	p := new(int) // want "^rc-shared: go$"
	go use(p)
}
