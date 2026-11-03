package census

import (
	"slices"
	"testing"

	"golang.org/x/tools/go/ssa"
)

// GOROOT/test issue31546.go, issue19113.go, issue15528.go: reflect.ValueOf(x)
// кладе x у коробку інтерфейсу, unpackEface дістає слово даних через
// unsafe.Pointer(&i), а Field зсуває вказівник через uintptr (add).
// Копія llgo reflect у мініатюрі: коробку не можна звільнити до Int.
const reflectFieldSrc = `package p

import "unsafe"

type eface struct {
	typ  *int
	word unsafe.Pointer
}

type Value struct {
	typ *int
	ptr unsafe.Pointer
}

func add(p unsafe.Pointer, x uintptr) unsafe.Pointer { return unsafe.Pointer(uintptr(p) + x) }

func unpack(i any) Value {
	e := (*eface)(unsafe.Pointer(&i))
	return Value{e.typ, e.word}
}

func ValueOf(i any) Value { return unpack(i) }

func (v Value) Field(off uintptr) Value { return Value{v.typ, add(v.ptr, off)} }

func (v Value) Int() int { return *(*int)(v.ptr) }

type T struct{ a, b, c int }

var x = T{1, 2, 3}

func use() int { return ValueOf(x).Field(16).Int() }
`

func TestReflectFieldKeepsBox(t *testing.T) {
	pkg := buildOnePackage(t, "example.com/p", reflectFieldSrc, ssa.SanityCheckFunctions)
	funcs := ssaFuncs(pkg)
	for _, m := range []string{"Field", "Int"} {
		funcs = append(funcs, pkg.Prog.LookupMethod(pkg.Type("Value").Type(), pkg.Pkg, m))
	}
	place := PlaceAll(funcs)
	use := pkg.Func("use")
	var box *ssa.MakeInterface
	var intCall *ssa.Call
	for _, b := range use.Blocks {
		for _, instr := range b.Instrs {
			switch instr := instr.(type) {
			case *ssa.MakeInterface:
				box = instr
			case *ssa.Call:
				if callee := instr.Call.StaticCallee(); callee != nil && callee.Name() == "Int" {
					intCall = instr
				}
			}
		}
	}
	if box == nil || intCall == nil {
		t.Fatalf("use: box %v, Int call %v; want both", box, intCall)
	}
	label := place.Cats[box]
	uses := append(place.StaticUses[box], place.DecUses[box]...)
	freed := len(place.StaticUses[box]) > 0 || len(place.DecUses[box]) > 0
	if freed && !slices.Contains(uses, ssa.Instruction(intCall)) {
		t.Errorf("PlaceAll(use): box %q freed after %v, want after %v or not freed", label, uses, intCall)
	}
}

