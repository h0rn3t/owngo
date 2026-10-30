package census

import (
	"go/token"
	"slices"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// Свіжість, яку порахував PublishProgram для всієї програми. Функція і її
// викликач мусять бачити одне значення: викликач зменшує слово, яке пише
// лише AllocCounted функції.
var (
	freshProg *ssa.Program
	freshBy   map[*ssa.Function]bool
)

// freshResults повертає функції funcs зі свіжим результатом. Якщо
// PublishProgram уже порахував цю програму, значення береться звідти.
func freshResults(funcs []*ssa.Function) map[*ssa.Function]bool {
	programMu.Lock()
	prog, by := freshProg, freshBy
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
	for _, fn := range funcs {
		if freshCandidate(fn) {
			out[fn] = true
		}
	}
	// Найбільша нерухома точка: результат свіжий, доки кожен return
	// лишається власним об'єктом за поточним набором.
	for changed := true; changed; {
		changed = false
		for fn := range out {
			if !freshBody(fn, out) {
				delete(out, fn)
				changed = true
			}
		}
	}
	return out
}

func publishFresh(prog *ssa.Program, by map[*ssa.Function]bool) {
	programMu.Lock()
	freshProg, freshBy = prog, by
	programMu.Unlock()
}

// freshCandidate: тіло є, функція не узагальнена і не з рантайму llgo,
// результат один і це вказівник. MakeMap, MakeChan і подібні викликача
// звільняються FreeAccounted з початку блока, тож рантайм слова не дає.
func freshCandidate(fn *ssa.Function) bool {
	if fn == nil || fn.Blocks == nil || fn.Pkg == nil || fn.TypeParams().Len() > 0 || fn.Origin() != nil {
		return false
	}
	path := fn.Pkg.Pkg.Path()
	if path == "github.com/xgo-dev/llgo/runtime" || strings.HasPrefix(path, "github.com/xgo-dev/llgo/runtime/") {
		return false
	}
	results := fn.Signature.Results()
	return results.Len() == 1 && pointee(results.At(0).Type()) != nil
}

func freshBody(fn *ssa.Function, fresh map[*ssa.Function]bool) bool {
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			ret, ok := instr.(*ssa.Return)
			if ok && !ownedResult(ret.Results[0], fresh, map[ssa.Value]bool{}) {
				return false
			}
		}
	}
	return true
}

// freshAllocs повертає купові Alloc, які свіжа функція повертає.
func freshAllocs(fn *ssa.Function) []*ssa.Alloc {
	var out []*ssa.Alloc
	seen := map[ssa.Value]bool{}
	var walk func(ssa.Value)
	walk = func(v ssa.Value) {
		if seen[v] {
			return
		}
		seen[v] = true
		switch v := v.(type) {
		case *ssa.Phi:
			for _, edge := range v.Edges {
				walk(edge)
			}
		case *ssa.Alloc:
			out = append(out, v)
		}
	}
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if ret, ok := instr.(*ssa.Return); ok {
				walk(ret.Results[0])
			}
		}
	}
	return out
}

// ownedResult: v — власний купový Alloc, свіжий результат іншого виклику
// або phi таких значень, і нічого, крім повернення і власних полів, його не бачить.
func ownedResult(v ssa.Value, fresh map[*ssa.Function]bool, seen map[ssa.Value]bool) bool {
	if seen[v] {
		return true
	}
	seen[v] = true
	switch v := v.(type) {
	case *ssa.Phi:
		for _, edge := range v.Edges {
			if !ownedResult(edge, fresh, seen) {
				return false
			}
		}
		return true
	case *ssa.Alloc:
		return v.Heap && ownedUses(v, fresh, map[ssa.Value]bool{})
	case *ssa.Call:
		return freshCall(v, fresh) && ownedUses(v, fresh, map[ssa.Value]bool{})
	default:
		return false
	}
}

func freshCall(call *ssa.Call, fresh map[*ssa.Function]bool) bool {
	callee := call.Call.StaticCallee()
	if callee == nil {
		return false
	}
	if fresh[callee] {
		return true
	}
	// Функція поза цим набором свіжа лише за публікацією тієї ж програми.
	programMu.Lock()
	defer programMu.Unlock()
	return freshProg == callee.Prog && freshBy[callee]
}

// ownedUses: v повертають, порівнюють, кладуть у phi, що далі так само,
// або пишуть і читають його поля. У поле обходу йде nil чи свіжий результат,
// який більше ніде не вживається; поле обходу не читається, бо інакше
// дитина, яку звільнить власник, вийшла б назовні.
func ownedUses(v ssa.Value, fresh map[*ssa.Function]bool, seen map[ssa.Value]bool) bool {
	if seen[v] {
		return true
	}
	seen[v] = true
	refs := v.Referrers()
	if refs == nil {
		return true
	}
	ring := CyclicFields(pointee(v.Type()))
	for _, ref := range *refs {
		switch ref := ref.(type) {
		case *ssa.Return, *ssa.DebugRef:
		case *ssa.Phi:
			if !ownedUses(ref, fresh, seen) {
				return false
			}
		case *ssa.BinOp:
			if ref.Op != token.EQL && ref.Op != token.NEQ {
				return false
			}
		case *ssa.Store:
			if ref.Addr != v {
				return false
			}
			if _, ok := ref.Val.(*ssa.Const); !ok {
				return false
			}
		case *ssa.FieldAddr:
			if !ownedField(ref, slices.Contains(ring, ref.Field), fresh) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func ownedField(addr *ssa.FieldAddr, ring bool, fresh map[*ssa.Function]bool) bool {
	refs := addr.Referrers()
	if refs == nil {
		return true
	}
	for _, ref := range *refs {
		switch ref := ref.(type) {
		case *ssa.DebugRef:
		case *ssa.UnOp:
			if ring || ref.Op != token.MUL {
				return false
			}
		case *ssa.Store:
			if ref.Addr != addr {
				return false
			}
			if ring && !movedChild(ref.Val, ref, fresh) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// movedChild: nil або свіжий результат виклику, єдине вживання якого — цей запис.
func movedChild(v ssa.Value, store *ssa.Store, fresh map[*ssa.Function]bool) bool {
	if c, ok := v.(*ssa.Const); ok {
		return c.IsNil()
	}
	call, ok := v.(*ssa.Call)
	if !ok || !freshCall(call, fresh) {
		return false
	}
	refs := call.Referrers()
	return refs != nil && len(*refs) == 1 && (*refs)[0] == store
}
