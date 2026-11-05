package census

import (
	"go/token"

	"golang.org/x/tools/go/ssa"
)

// regionRoot: site — виклик будівника (P-NEW-05.1), а дерево після побудови
// лише читають у цій функції. Тоді власник — викликач, як для DecCyclic P6.6
// (повернення, panic і будь-яка інша втеча — не читання), і все дерево може
// жити в одному регіоні, який звільняється в точці смерті кореня (P-NEW-05.2).
func regionRoot(site ssa.Instruction, v1 *v1Ctx) bool {
	call, ok := site.(*ssa.Call)
	if !ok {
		return false
	}
	callee := call.Call.StaticCallee()
	if callee == nil {
		return false
	}
	if sum := summaryOf(v1, callee); sum == nil || !sum.Builder {
		return false
	}
	return readOnlyTree(call, v1, map[ssa.Value]bool{})
}

// readOnlyTree: v — корінь чи вказівник, завантажений з поля обходу іншого
// такого вказівника, і жоден з них не пишуть, не зберігають, не повертають,
// не захоплюють, не несе phi і не отримує виклик, що його тримає чи пише.
func readOnlyTree(v ssa.Value, v1 *v1Ctx, seen map[ssa.Value]bool) bool {
	if seen[v] {
		return true
	}
	seen[v] = true
	for _, ref := range *v.Referrers() {
		switch ref := ref.(type) {
		case *ssa.DebugRef, *ssa.BinOp:
			// Вказівники Go лише порівнюють на рівність.
		case *ssa.Call:
			if !regionArg(&ref.Call, v, v1) {
				return false
			}
		case *ssa.Defer:
			if !regionArg(&ref.Call, v, v1) {
				return false
			}
		case *ssa.FieldAddr:
			if !readOnlyField(ref, v1, seen) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// readOnlyField: поле лише читають; прочитаний вказівник з поля обходу — теж
// частина дерева. Інших вказівників тип будівника не має.
func readOnlyField(addr *ssa.FieldAddr, v1 *v1Ctx, seen map[ssa.Value]bool) bool {
	for _, ref := range *addr.Referrers() {
		switch ref := ref.(type) {
		case *ssa.DebugRef:
		case *ssa.UnOp:
			if ref.Op != token.MUL {
				return false
			}
			if ringField(addr) && !readOnlyTree(ref, v1, seen) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// regionArg: виклик лише читає v за зведенням (P6.7) і не повертає його:
// результат-аліас вивів би вузол з-під контролю кореня. Закріплений параметр
// має й Writes, тож його відкидає readOnlyArg.
func regionArg(call *ssa.CallCommon, v ssa.Value, v1 *v1Ctx) bool {
	if !readOnlyArg(call, v, v1) {
		return false
	}
	sum := summaryOf(v1, call.StaticCallee())
	for i, arg := range call.Args {
		if arg != v {
			continue
		}
		if p := sum.Params[summaryIndex(call, i)]; len(p.Alias) > 0 {
			return false
		}
	}
	return true
}
