package census

import (
	"testing"

	"golang.org/x/tools/go/ssa"
)

// GOROOT/test fixedbugs/issue15528.go: fmt.Stringer(Strunger(Int(5))) у
// глобальному масиві — ChangeInterface несе ту саму коробку. Без цього
// коробку звільняло в init, а main читала її через reflect.
const changeIfaceSrc = `package p

type Str interface{ String() string }
type Strunger interface {
	Str
	Strung()
}
type Int int

func (i Int) String() string { return "" }
func (i Int) Strung()        {}

var g Str

func set() { g = Str(Strunger(Int(5))) }
`

func TestChangeInterfaceCarriesBox(t *testing.T) {
	pkg := buildOnePackage(t, "example.com/p", changeIfaceSrc, ssa.SanityCheckFunctions)
	place := PlaceAll(ssaFuncs(pkg))
	var box ssa.Instruction
	for _, b := range pkg.Func("set").Blocks {
		for _, instr := range b.Instrs {
			if mi, ok := instr.(*ssa.MakeInterface); ok {
				box = mi
			}
		}
	}
	if box == nil {
		t.Fatal("set: no MakeInterface")
	}
	if got, want := place.Cats[box], "rc-shared: global"; got != want {
		t.Errorf("PlaceAll(set).Cats[Strunger(Int(5))] = %q, want %q", got, want)
	}
}
