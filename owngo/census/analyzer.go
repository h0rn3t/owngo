package census

import (
	"fmt"
	"go/token"
	"go/types"
	"reflect"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/buildssa"
	"golang.org/x/tools/go/ssa"
)

// Site — місце алокації в результаті аналізатора і в JSON команди.
//
// Порожній Reason лишається ключем JSON: stack, static і cyclic без втечі
// не мають причини.
type Site struct {
	Category string `json:"category"` // категорія v0
	Reason   string `json:"reason"`   // причина після ": ", або порожньо
	Pos      string `json:"pos"`      // file:line без колонки
	Type     string `json:"type"`     // тип значення SSA
}

// Analyzer знаходить місця алокації пакета і звітує категорію v0.
// Прапорець -v1 замінює причину call на позичання, переміщення або спільне.
// Result має тип []Site, порожній зріз, якщо місць немає.
var Analyzer = &analysis.Analyzer{
	Name:       "census",
	Doc:        "класифікує місця алокації Go-програми",
	Requires:   []*analysis.Analyzer{buildssa.Analyzer},
	ResultType: reflect.TypeFor[[]Site](),
	Run:        run,
}

func init() {
	Analyzer.Flags.Bool("v1", false, "зведення параметрів: позичання, переміщення, спільне")
}

func v1On(pass *analysis.Pass) bool {
	f := pass.Analyzer.Flags.Lookup("v1")
	return f != nil && f.Value.String() == "true"
}

func run(pass *analysis.Pass) (any, error) {
	pkg := pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA)
	mode := ifaceStrict
	var universe []types.Type
	if f := pass.Analyzer.Flags.Lookup("conservative"); f != nil && f.Value.String() == "true" {
		mode = ifaceConservative
		for _, name := range pass.Pkg.Scope().Names() {
			obj, ok := pass.Pkg.Scope().Lookup(name).(*types.TypeName)
			if !ok {
				continue
			}
			universe = append(universe, obj.Type(), types.NewPointer(obj.Type()))
		}
	}
	var ctx *v1Ctx
	if v1On(pass) {
		ctx = &v1Ctx{sums: packageSummaries(pass, pkg.SrcFuncs), pass: pass}
	}
	sites := []Site{}
	for _, fn := range pkg.SrcFuncs {
		if fn.Blocks == nil {
			continue
		}
		msg := messages(fn, mode, universe, ctx)
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if !isSite(instr) {
					continue
				}
				text := msg[instr]
				category, reason, _ := strings.Cut(text, ": ")
				value, ok := instr.(ssa.Value)
				if !ok {
					return nil, fmt.Errorf("місце %T не є значенням SSA", instr)
				}
				pos := pass.Fset.Position(sitePos(instr))
				sites = append(sites, Site{
					Category: category,
					Reason:   reason,
					Pos:      fmt.Sprintf("%s:%d", pos.Filename, pos.Line),
					Type:     types.TypeString(value.Type(), types.RelativeTo(pass.Pkg)),
				})
				pass.Reportf(sitePos(instr), "%s", text)
			}
		}
	}
	return sites, nil
}

func isSite(instr ssa.Instruction) bool {
	switch instr := instr.(type) {
	case *ssa.Alloc, *ssa.MakeSlice, *ssa.MakeMap, *ssa.MakeChan:
		return true
	case *ssa.MakeClosure:
		return len(instr.Bindings) > 0
	case *ssa.MakeInterface:
		_, ptr := instr.X.Type().Underlying().(*types.Pointer)
		return !ptr
	case *ssa.Call:
		builtin, ok := instr.Call.Value.(*ssa.Builtin)
		return ok && builtin.Name() == "append"
	case *ssa.BinOp:
		return instr.Op == token.ADD && isString(instr.X.Type())
	case *ssa.Convert:
		return stringSliceConvert(instr.X.Type(), instr.Type())
	default:
		return false
	}
}

// sitePos бере Pos інструкції. У літерала функції MakeClosure часто має
// невалідний Pos: тоді це позиція ключового слова func самої функції SSA.
func sitePos(instr ssa.Instruction) token.Pos {
	closure, ok := instr.(*ssa.MakeClosure)
	if !ok || instr.Pos().IsValid() {
		return instr.Pos()
	}
	fn, ok := closure.Fn.(*ssa.Function)
	if !ok {
		return instr.Pos()
	}
	return fn.Pos()
}

func isString(t types.Type) bool {
	basic, ok := t.Underlying().(*types.Basic)
	return ok && basic.Kind() == types.String
}

func stringSliceConvert(from, to types.Type) bool {
	return isString(from) && byteOrRuneSlice(to) || isString(to) && byteOrRuneSlice(from)
}

func byteOrRuneSlice(t types.Type) bool {
	slice, ok := t.Underlying().(*types.Slice)
	if !ok {
		return false
	}
	basic, ok := slice.Elem().Underlying().(*types.Basic)
	return ok && (basic.Kind() == types.Byte || basic.Kind() == types.Rune)
}
