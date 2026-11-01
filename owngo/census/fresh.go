package census

import (
	"go/token"
	"go/types"
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
// результат один і це вказівник чи зріз (зріз — лише внутрішній). MakeMap, MakeChan і подібні викликача
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
	if results.Len() != 1 {
		return false
	}
	_, slice := results.At(0).Type().Underlying().(*types.Slice)
	return slice || pointee(results.At(0).Type()) != nil
}

// freshBody: кожен return віддає власний об'єкт. Внутрішній результат не
// змішується з базовим, і всі його бази одного типу: викликач бере розмір
// і дескриптор з цього типу.
func freshBody(fn *ssa.Function, fresh map[*ssa.Function]bool) bool {
	interior := returnsInterior(fn)
	var base types.Type
	for _, ret := range returns(fn) {
		if !interior {
			if !ownedResult(ret.Results[0], fresh, map[ssa.Value]bool{}) {
				return false
			}
			continue
		}
		alloc := ownedInterior(ret.Results[0], fresh)
		if alloc == nil || base != nil && !types.Identical(base, alloc.Type()) {
			return false
		}
		base = alloc.Type()
	}
	return true
}

func returns(fn *ssa.Function) []*ssa.Return {
	var out []*ssa.Return
	for _, block := range fn.Blocks {
		if ret, ok := block.Instrs[len(block.Instrs)-1].(*ssa.Return); ok {
			out = append(out, ret)
		}
	}
	return out
}

// returnsInterior: хоч один return віддає &x.f чи x[i:j].
func returnsInterior(fn *ssa.Function) bool {
	for _, ret := range returns(fn) {
		if len(ret.Results) == 1 && interiorBase(ret.Results[0]) != nil {
			return true
		}
	}
	return false
}

// InteriorBase повертає тип об'єкта, на який вказує свіжий внутрішній
// результат виклику call, або nil, якщо результат не внутрішній чи тіла
// викликаної функції немає. Слово цього об'єкта знаходить runtime.Base.
func InteriorBase(call *ssa.Call) types.Type {
	callee := call.Call.StaticCallee()
	if callee == nil || callee.Blocks == nil || !returnsInterior(callee) {
		return nil
	}
	for _, ret := range returns(callee) {
		if alloc := interiorBase(ret.Results[0]); alloc != nil {
			return pointee(alloc.Type())
		}
	}
	return nil
}

// interiorBase повертає Alloc в основі ланцюга FieldAddr і Slice з хоча б
// одного кроку, або nil.
func interiorBase(v ssa.Value) *ssa.Alloc {
	steps := 0
	for {
		switch x := v.(type) {
		case *ssa.FieldAddr:
			v = x.X
		case *ssa.Slice:
			v = x.X
		case *ssa.Alloc:
			if steps == 0 {
				return nil
			}
			return x
		default:
			return nil
		}
		steps++
	}
}

// ownedInterior повертає купový Alloc x, якщо v — &x.f чи x[i:j] всередині
// x, а x і похідні від нього лише повертаються, читаються, пишуться і
// порівнюються. Поле обходу не повертається і не читається, як в ownedUses.
func ownedInterior(v ssa.Value, fresh map[*ssa.Function]bool) *ssa.Alloc {
	alloc := interiorBase(v)
	if alloc == nil || !alloc.Heap || !insideBlock(v) || !derivedUses(alloc, alloc, fresh) {
		return nil
	}
	return alloc
}

// insideBlock: вказівник ланцюга лежить усередині блока, а не на його кінці,
// де runtime.Base знайшов би наступний блок. Поле нульового розміру і зріз
// з невідомим чи крайнім low відкидаються.
func insideBlock(v ssa.Value) bool {
	low := int64(0)
	for {
		switch x := v.(type) {
		case *ssa.FieldAddr:
			if sizes.Sizeof(pointee(x.Type())) == 0 {
				return false
			}
			low, v = 0, x.X
			continue
		case *ssa.Slice:
			if x.Low != nil {
				c, ok := x.Low.(*ssa.Const)
				if !ok {
					return false
				}
				low += c.Int64()
			}
			v = x.X
			if _, ok := v.(*ssa.Slice); ok {
				continue
			}
			arr, ok := pointee(v.Type()).Underlying().(*types.Array)
			if !ok || low >= arr.Len() {
				return false
			}
			low = 0
			continue
		}
		return true
	}
}

// sizes лише відрізняє нульовий розмір, а він від платформи не залежить.
var sizes = types.SizesFor("gc", "amd64")

// derivedUses: val — base чи похідний від нього вказівник, і кожне його
// вживання тримає об'єкт у функції або повертає похідний вказівник.
func derivedUses(val ssa.Value, base *ssa.Alloc, fresh map[*ssa.Function]bool) bool {
	refs := val.Referrers()
	if refs == nil {
		return true
	}
	for _, ref := range *refs {
		switch ref := ref.(type) {
		case *ssa.DebugRef:
		case *ssa.Return:
			if val == base {
				return false
			}
		case *ssa.BinOp:
			if ref.Op != token.EQL && ref.Op != token.NEQ {
				return false
			}
		case *ssa.UnOp:
			if ref.Op != token.MUL {
				return false
			}
		case *ssa.Store:
			if ref.Addr != val {
				return false
			}
			// Запис цілого об'єкта копіює й поля обходу, тож лише константа.
			if _, ok := ref.Val.(*ssa.Const); val == base && !ok {
				return false
			}
		case *ssa.FieldAddr:
			if ringField(ref) {
				if !ownedField(ref, true, fresh) {
					return false
				}
				continue
			}
			if !derivedUses(ref, base, fresh) {
				return false
			}
		case *ssa.IndexAddr:
			if !derivedUses(ref, base, fresh) {
				return false
			}
		case *ssa.Slice:
			if !derivedUses(ref, base, fresh) {
				return false
			}
		case *ssa.Call:
			name, ok := builtinName(&ref.Call)
			if !ok || name != "len" && name != "cap" && name != "copy" {
				return false
			}
		default:
			return false
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
		case *ssa.FieldAddr, *ssa.Slice:
			if alloc := interiorBase(v); alloc != nil {
				out = append(out, alloc)
			}
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
	// Внутрішній результат — не початок об'єкта: ні повернути його як свій,
	// ні перемістити в поле обходу не можна.
	if callee == nil || callee.Blocks != nil && returnsInterior(callee) {
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
	if refs == nil {
		return false
	}
	// DebugRef з ssa.GlobalDebug значення не тримає.
	for _, ref := range *refs {
		if _, debug := ref.(*ssa.DebugRef); !debug && ref != store {
			return false
		}
	}
	return true
}

// freshSites повертає статичні виклики зі свіжим результатом за зведенням
// v1. Без v1 (v0, корпус) таких місць немає.
func (v1 *v1Ctx) freshSites(instrs []ssa.Instruction) map[ssa.Instruction]bool {
	if v1 == nil {
		return nil
	}
	var out map[ssa.Instruction]bool
	for _, instr := range instrs {
		call, ok := instr.(*ssa.Call)
		if !ok || call.Call.StaticCallee() == nil {
			continue
		}
		sum := summaryOf(v1, call.Call.StaticCallee())
		if sum == nil || !sum.Fresh || sum.Interior && InteriorBase(call) == nil {
			continue
		}
		if out == nil {
			out = map[ssa.Instruction]bool{}
		}
		out[instr] = true
	}
	return out
}

// freshSite: місце — свіжий виклик, а не append. Слово дав викликаний код,
// тож таке місце звільняє лише Dec.
func freshSite(site ssa.Instruction) bool {
	call, ok := site.(*ssa.Call)
	return ok && ImplicitOrigin(call) == ""
}
