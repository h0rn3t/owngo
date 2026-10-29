package census

import (
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/buildssa"
	"golang.org/x/tools/go/ssa"
)

// Analyzer знаходить місця алокації пакета і звітує unknown.
// Текст категорій із корпусу з'явиться в P0.4.
var Analyzer = &analysis.Analyzer{
	Name:     "census",
	Doc:      "класифікує місця алокації Go-програми",
	Requires: []*analysis.Analyzer{buildssa.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	pkg := pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA)
	for _, fn := range pkg.SrcFuncs {
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if !isSite(instr) {
					continue
				}
				pass.Reportf(sitePos(instr), "unknown")
			}
		}
	}
	return nil, nil
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
