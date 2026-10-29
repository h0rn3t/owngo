package corpus

// Цикл через поле-вказівник. Локальне використання не робить місце static.
type list struct{ next *list }

func cycleList() {
	n := &list{} // want "^cyclic$"
	n.next = n
	_ = n.next
}

type tree struct {
	parent *tree
	kids   []*tree
}

func cycleTree() {
	t := &tree{} // want "^cyclic$"
	t.parent = t
	_ = t.kids
}

type digraph struct{ out []*digraph }

func cyclePtrSlice() {
	g := &digraph{} // want "^cyclic$"
	_ = g.out
}

// Цикл через слайс значень того самого типу.
type ring struct{ xs []ring }

func cycleSliceVal() {
	r := &ring{} // want "^cyclic$"
	_ = r.xs
}

type bucket struct{ m map[int]*bucket }

func cycleMap() {
	b := &bucket{} // want "^cyclic$"
	_ = b.m
}

func cycleReturn() *list {
	n := &list{} // want "^cyclic: return$"
	return n
}

var root *list

func cycleGlobal() {
	root = &list{} // want "^cyclic: global$"
}

// Строгий режим: поле-інтерфейс не замикає тип на себе.
type holder struct{ v any }

func strictIfaceField() {
	h := &holder{} // want "^static$"
	_ = h.v
}
