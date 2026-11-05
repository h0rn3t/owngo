package census

import (
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/ssa"
)

// builderResults повертає будівники серед свіжих функцій fresh (P-NEW-05.1):
// їхній результат — структура лише з власних Alloc і переміщених результатів
// інших будівників, тож її можна виділити в регіоні. Якщо PublishProgram
// уже порахував цю програму, значення береться звідти.
func builderResults(funcs []*ssa.Function, fresh map[*ssa.Function]bool) map[*ssa.Function]bool {
	programMu.Lock()
	prog, by := freshProg, builderBy
	programMu.Unlock()
	out := map[*ssa.Function]bool{}
	if prog != nil && len(funcs) > 0 && funcs[0] != nil && funcs[0].Prog == prog {
		for _, fn := range funcs {
			if by[fn] {
				out[fn] = true
			}
		}
		return out
	}
	for fn := range fresh {
		if !returnsInterior(fn) {
			out[fn] = true
		}
	}
	// Та сама найбільша нерухома точка, що у freshResults: взаємна рекурсія
	// лишається будівником, доки жодне тіло не порушує умов.
	for changed := true; changed; {
		changed = false
		for fn := range out {
			if !builderBody(fn, fresh, out) {
				delete(out, fn)
				changed = true
			}
		}
	}
	return out
}

// builderBody перевіряє умови будівника поверх свіжості fn (P6.5). Умови,
// які свіжість уже дає, — у поле обходу пишуться лише nil і переміщені
// свіжі результати, — тут не повторюються.
func builderBody(fn *ssa.Function, fresh, builders map[*ssa.Function]bool) bool {
	returned := returnedValues(fn)
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			switch instr := instr.(type) {
			case *ssa.Alloc:
				if instr.Heap && (!returned[instr] || containsRefOutsideRing(pointee(instr.Type()))) {
					return false
				}
			case *ssa.Call:
				if !builderCall(instr, returned, fresh, builders) {
					return false
				}
			case *ssa.Store:
				if !localAddr(instr.Addr) {
					return false
				}
			case *ssa.Go, *ssa.Defer, *ssa.Send, *ssa.Select, *ssa.MakeChan,
				*ssa.MakeSlice, *ssa.MakeMap, *ssa.MapUpdate:
				return false
			case *ssa.UnOp:
				if instr.Op == token.ARROW {
					return false
				}
			case *ssa.BinOp:
				if isString(instr.X.Type()) && instr.Op == token.ADD {
					return false
				}
			case *ssa.Convert:
				if isUnsafePointer(instr.X.Type()) || isUnsafePointer(instr.Type()) {
					return false
				}
			}
		}
	}
	return true
}

// builderCall: вбудована функція без запису й алокації або статичний
// виклик. Свіжий результат дає лише будівник, і цей результат повертають
// або переміщують у поле обходу повернутого Alloc.
func builderCall(call *ssa.Call, returned map[ssa.Value]bool, fresh, builders map[*ssa.Function]bool) bool {
	if name, ok := builtinName(&call.Call); ok {
		return slices.Contains([]string{"len", "cap", "min", "max", "print", "println"}, name)
	}
	callee := call.Call.StaticCallee()
	if callee == nil {
		return false
	}
	if !freshCall(call, fresh) {
		return true
	}
	if !builders[callee] && !publishedBuilder(callee) {
		return false
	}
	return returned[call] || movedIntoReturned(call, returned)
}

func publishedBuilder(fn *ssa.Function) bool {
	programMu.Lock()
	defer programMu.Unlock()
	return freshProg == fn.Prog && builderBy[fn]
}

// movedIntoReturned: єдине вживання call — запис у поле обходу повернутого Alloc.
func movedIntoReturned(call *ssa.Call, returned map[ssa.Value]bool) bool {
	var store *ssa.Store
	for _, ref := range *call.Referrers() {
		switch ref := ref.(type) {
		case *ssa.DebugRef:
		case *ssa.Store:
			if store != nil {
				return false
			}
			store = ref
		default:
			return false
		}
	}
	if store == nil || store.Val != call || !ringField(store.Addr) {
		return false
	}
	_, isAlloc := store.Addr.(*ssa.FieldAddr).X.(*ssa.Alloc)
	return isAlloc && returned[store.Addr.(*ssa.FieldAddr).X]
}

// returnedValues повертає Alloc і виклики, які fn повертає, крізь phi.
func returnedValues(fn *ssa.Function) map[ssa.Value]bool {
	out := map[ssa.Value]bool{}
	var walk func(ssa.Value)
	walk = func(v ssa.Value) {
		if out[v] {
			return
		}
		out[v] = true
		if phi, ok := v.(*ssa.Phi); ok {
			for _, edge := range phi.Edges {
				walk(edge)
			}
		}
	}
	for _, ret := range returns(fn) {
		walk(ret.Results[0])
	}
	return out
}

// localAddr: адреса — власний Alloc функції чи його поле або елемент, а не
// глобальна змінна, параметр чи захоплена змінна.
func localAddr(addr ssa.Value) bool {
	for {
		switch x := addr.(type) {
		case *ssa.FieldAddr:
			addr = x.X
		case *ssa.IndexAddr:
			addr = x.X
		case *ssa.Alloc:
			return true
		default:
			return false
		}
	}
}

// containsRefOutsideRing: у t є вказівник поза полями обходу DecCyclic.
// Регіон звільняє лише вузли, тож інший вказівник з вузла тримав би чужий об'єкт.
func containsRefOutsideRing(t types.Type) bool {
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		return containsRef(t)
	}
	ring := CyclicFields(t)
	for i := range st.NumFields() {
		if !slices.Contains(ring, i) && containsRef(st.Field(i).Type()) {
			return true
		}
	}
	return false
}

func isUnsafePointer(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Kind() == types.UnsafePointer
}
