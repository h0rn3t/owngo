package census

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

// TestCyclicTypes перевіряє граф типів у двох режимах.
//
// Строгий: ребра — вказівник, елемент слайса чи масиву, значення мапи,
// поля структури. Інтерфейс, канал, функція і ключ мапи ребра не дають.
//
// Консервативний: до тих самих ребер інтерфейс додає ребро до кожного
// конкретного типу всесвіту, який можна йому присвоїти. Всесвіт тут —
// іменовані типи пакета і вказівники на них. Тип з іншого пакета, який
// реалізує інтерфейс і тримає зворотний вказівник, у всесвіт не входить.
func TestCyclicTypes(t *testing.T) {
	pkg := checkTypes(t, `
package p

type list struct{ next *list }

type tree struct {
	parent *tree
	kids   []*tree
}

type digraph struct{ out []*digraph }

type ring struct{ xs []ring }

type bucket struct{ m map[int]*bucket }

type grid struct{ cells [2]*grid }

type left struct{ right *right }
type right struct{ left *left }

type leaf struct{ n int }
type stem struct{ leaf *leaf }

type pipe struct{ c chan *pipe }
type thunk struct{ f func() *thunk }
type keyset struct{ m map[*keyset]int }

type holder struct{ v any }

type linker interface{ link() }
type cell struct{ next linker }

func (*cell) link() {}

type edge interface{ edge() }
type vertex struct{ out []edge }

func (*vertex) edge() {}

type hand interface{ hand() }
type alpha struct{ peer hand }
type beta struct{ peer hand }

func (*alpha) hand() {}
func (*beta) hand() {}

type reader interface{ Read([]byte) (int, error) }
type quiet struct{ r reader }

type label struct{ s string }

func (label) String() string { return "" }

type stringer interface{ String() string }
type tagged struct{ v stringer }

type bomb struct{ err error }

func (bomb) Error() string { return "" }

type box struct{ r reader }
type payload struct{ box *box }

func (payload) Read([]byte) (int, error) { return 0, nil }
`)
	universe := pkgUniverse(pkg)

	tests := []struct {
		typ          string
		strict       bool
		conservative bool
	}{
		{typ: "list", strict: true, conservative: true},
		{typ: "tree", strict: true, conservative: true},
		{typ: "digraph", strict: true, conservative: true},
		{typ: "ring", strict: true, conservative: true},
		{typ: "bucket", strict: true, conservative: true},
		{typ: "grid", strict: true, conservative: true},
		{typ: "left", strict: true, conservative: true},
		{typ: "right", strict: true, conservative: true},

		{typ: "leaf", strict: false, conservative: false},
		{typ: "stem", strict: false, conservative: false},
		{typ: "pipe", strict: false, conservative: false},
		{typ: "thunk", strict: false, conservative: false},
		{typ: "keyset", strict: false, conservative: false},

		{typ: "holder", strict: false, conservative: true},
		{typ: "cell", strict: false, conservative: true},
		{typ: "vertex", strict: false, conservative: true},
		{typ: "alpha", strict: false, conservative: true},
		{typ: "beta", strict: false, conservative: true},
		{typ: "bomb", strict: false, conservative: true},
		{typ: "box", strict: false, conservative: true},
		{typ: "payload", strict: false, conservative: true},

		{typ: "quiet", strict: false, conservative: false},
		{typ: "tagged", strict: false, conservative: false},
	}
	for _, tt := range tests {
		t.Run(tt.typ, func(t *testing.T) {
			t.Parallel()
			site := types.NewPointer(pkg.Scope().Lookup(tt.typ).(*types.TypeName).Type())
			for _, mode := range []struct {
				name string
				mode ifaceMode
				want bool
			}{
				{name: "strict", mode: ifaceStrict, want: tt.strict},
				{name: "conservative", mode: ifaceConservative, want: tt.conservative},
			} {
				got := cyclic(site, mode.mode, universe)
				if got != mode.want {
					t.Errorf("cyclic(%s, %s) = %t, want %t", tt.typ, mode.name, got, mode.want)
				}
			}
		})
	}
}

func TestCyclicNil(t *testing.T) {
	if got := cyclic(nil, ifaceStrict, nil); got {
		t.Errorf("cyclic(nil, strict) = %t, want false", got)
	}
	if got := cyclic(nil, ifaceConservative, nil); got {
		t.Errorf("cyclic(nil, conservative) = %t, want false", got)
	}
}

func checkTypes(t *testing.T, src string) *types.Package {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatalf("parser.ParseFile() error = %v", err)
	}
	pkg, err := new(types.Config).Check("p", fset, []*ast.File{file}, nil)
	if err != nil {
		t.Fatalf("types.Config.Check() error = %v", err)
	}
	return pkg
}

func pkgUniverse(pkg *types.Package) []types.Type {
	names := pkg.Scope().Names()
	out := make([]types.Type, 0, len(names)*2)
	for _, name := range names {
		obj, ok := pkg.Scope().Lookup(name).(*types.TypeName)
		if !ok {
			continue
		}
		out = append(out, obj.Type(), types.NewPointer(obj.Type()))
	}
	return out
}
