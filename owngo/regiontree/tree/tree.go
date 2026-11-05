// Package tree — будівник фікстури owngo/regiontree: дерево будує лише
// New, тож компілятор виводить для нього клон New$region (P-NEW-05.4).
package tree

// Tree — вузол повного бінарного дерева.
type Tree struct {
	Left, Right *Tree
}

// New будує повне дерево глибини depth.
func New(depth int) *Tree {
	if depth > 0 {
		return &Tree{Left: New(depth - 1), Right: New(depth - 1)}
	}
	return &Tree{}
}

// Count повертає кількість вузлів.
func (t *Tree) Count() int {
	if t.Left == nil {
		return 1
	}
	return 1 + t.Left.Count() + t.Right.Count()
}
