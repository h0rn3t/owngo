// Фікстура gcgap: кожен рядок з позначкою — один вид алокації.
package main

type pair struct{ a, b int }

var (
	sinkPair  *pair
	sinkInt   *int
	sinkInts  []int
	sinkFunc  func() int
	sinkSlice []*int
)

//go:noinline
func sum(xs ...int) int {
	s := 0
	for _, x := range xs {
		s += x
	}
	return s
}

//go:noinline
func keep(xs ...*int) { sinkSlice = xs }

//go:noinline
func call(f func() int) int { return f() }

func main() {
	p := new(int)                               // new: gc does not escape
	q := &pair{a: 1}                            // complit: gc does not escape
	sinkPair = &pair{b: 2}                      // complit: gc escapes
	n := sum(1, 2, *p, q.a)                     // args: gc does not escape
	keep(new(int), sinkPair.ptr())              // args: gc escapes
	sinkInts = append(sinkInts, n)              // append args: gc has no array
	var v int                                   // var: gc keeps on stack
	n += call(func() int { v++; return v + n }) // closure: gc does not escape
	var w int                                   // var: gc moves to heap
	sinkInt = &w
	sinkFunc = func() int { return w } // closure: gc escapes
	m := make([]int, 8)                // make: gc does not escape
	m[0] = n
	sinkInts = append(sinkInts, m[0]+m[1])
}

func (p *pair) ptr() *int { return &p.b }
