package census

import (
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// Shared описує місце спільної втечі через go з атомарним словом (P9.3).
type Shared struct {
	// Content — тип об'єкта, на який вказує вміст комірки місця, якщо
	// каскад DecShared проходить вміст; nil — не проходить. У комірку
	// пишуть лише nil і переміщений свіжий результат, а прочитаний вміст
	// лише читають.
	Content types.Type
	// ContentCyclic — дитину звільняє DecCyclic, інакше DecAccounted.
	ContentCyclic bool
}

// goPass — місце, яке go передає горутині прямо: зв'язкою замикання чи
// аргументом. inner — FreeVar чи параметр, яким горутина його бачить.
type goPass struct {
	instr *ssa.Go
	inner ssa.Value
}

// sharedSites знаходить у fn купові місця з позначкою rc-shared чи cyclic
// і причиною go, яким досить атомарного слова: без go позначка була б
// static, rc-local чи cyclic з причиною, за якою decSite ставить Dec;
// у go місце йде лише прямо, а горутина його лише позичає. Повертає
// опис місць, значення для IncShared і DecShared горутини на кожному go
// і читачів, після яких функція місця ставить DecShared.
func sharedSites(fn *ssa.Function, msg map[ssa.Instruction]string, pts map[ssa.Value]siteSet, contents map[ssa.Instruction]siteSet, ctx *v1Ctx) (map[ssa.Instruction]Shared, map[*ssa.Go][]ssa.Value, map[ssa.Instruction][]ssa.Instruction) {
	cand := siteSet{}
	for site, label := range msg {
		alloc, ok := site.(*ssa.Alloc)
		head, reason, _ := strings.Cut(label, ": ")
		if ok && alloc.Heap && reason == "go" && (head == "rc-shared" || head == "cyclic") {
			cand.add(site)
		}
	}
	if len(cand) == 0 {
		return nil, nil, nil
	}
	passes, blocked := goPasses(fn, pts, contents)

	// Позначка без go: той самий розбір, де go втечі не дає.
	shadow := &v1Ctx{sums: ctx.sums, pass: ctx.pass, seen: ctx.seen, skipGo: true}
	solo, soloPts, soloContents := messages(fn, ifaceStrict, nil, shadow)
	shadow.blockDec(exitSites(fn, soloPts, soloContents))
	for _, vals := range shadow.inc {
		for _, val := range vals {
			if instr, ok := val.(ssa.Instruction); ok {
				// Спільна передача ставить неатомарний Inc.
				blocked.add(instr)
			}
		}
	}

	var (
		shared  map[ssa.Instruction]Shared
		goVals  map[*ssa.Go][]ssa.Value
		readers map[ssa.Instruction][]ssa.Instruction
	)
	for site := range cand {
		if _, ok := blocked[site]; ok || len(passes[site]) == 0 {
			continue
		}
		label := solo[site]
		head, _, _ := strings.Cut(label, ":")
		if head != "static" && head != "rc-local" && head != "cyclic" || !decSite(site, label, soloPts, shadow) {
			continue
		}
		val := site.(ssa.Value)
		contentOK := cellContent(val, ctx)
		borrowed := true
		for _, pass := range passes[site] {
			if !borrowUses(pass.inner, ctx, map[ssa.Value]bool{}) {
				borrowed = false
				break
			}
			contentOK = contentOK && cellContent(pass.inner, ctx)
		}
		if !borrowed {
			continue
		}
		info := Shared{}
		if elem := pointee(pointee(val.Type())); contentOK && elem != nil {
			info.Content = elem
			_, isStruct := elem.Underlying().(*types.Struct)
			info.ContentCyclic = isStruct && cyclic(types.NewPointer(elem), ifaceStrict, nil)
		}
		if shared == nil {
			shared = map[ssa.Instruction]Shared{}
			goVals = map[*ssa.Go][]ssa.Value{}
			readers = map[ssa.Instruction][]ssa.Instruction{}
		}
		shared[site] = info
		for _, pass := range passes[site] {
			goVals[pass.instr] = appendNew(goVals[pass.instr], val)
		}
		readers[site] = sharedReaders(site, pts)
	}
	return shared, goVals, readers
}

// goPasses зводить для кожного місця, яке go бачить, його прямі передачі.
// Місце, досяжне з go не прямо (вміст, похідний вказівник, невідомий
// callee), і вміст переданого місця потрапляють у blocked.
func goPasses(fn *ssa.Function, pts map[ssa.Value]siteSet, contents map[ssa.Instruction]siteSet) (map[ssa.Instruction][]goPass, siteSet) {
	passes := map[ssa.Instruction][]goPass{}
	blocked := siteSet{}
	var block func(siteSet)
	block = func(sites siteSet) {
		for site := range sites {
			if blocked.add(site) {
				block(contents[site])
			}
		}
	}
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			g, ok := instr.(*ssa.Go)
			if !ok {
				continue
			}
			outer, inner := goOperands(g)
			if outer == nil {
				for _, arg := range g.Call.Args {
					block(pts[arg])
				}
				block(pts[g.Call.Value])
				continue
			}
			for i, v := range outer {
				for site := range pts[v] {
					for inner := range contents[site] {
						block(siteSet{inner: {}})
					}
					if v != site.(ssa.Value) {
						block(siteSet{site: {}})
						continue
					}
					passes[site] = append(passes[site], goPass{instr: g, inner: inner[i]})
				}
			}
		}
	}
	return passes, blocked
}

// goOperands повертає значення, які go передає, і те, чим їх бачить тіло
// горутини: зв'язки замикання, яке вживає лише цей go, і FreeVars, або
// аргументи статичного виклику і параметри. Інше дає nil.
func goOperands(g *ssa.Go) (outer, inner []ssa.Value) {
	if g.Call.IsInvoke() {
		return nil, nil
	}
	if mc, ok := g.Call.Value.(*ssa.MakeClosure); ok {
		fn, ok := mc.Fn.(*ssa.Function)
		if !ok || fn.Blocks == nil || len(g.Call.Args) != 0 || !onlyUser(mc, g) {
			return nil, nil
		}
		for i, binding := range mc.Bindings {
			outer = append(outer, binding)
			inner = append(inner, fn.FreeVars[i])
		}
		return outer, inner
	}
	fn, ok := g.Call.Value.(*ssa.Function)
	if !ok || fn.Blocks == nil || len(fn.Params) != len(g.Call.Args) {
		return nil, nil
	}
	for i, arg := range g.Call.Args {
		outer = append(outer, arg)
		inner = append(inner, fn.Params[i])
	}
	return outer, inner
}

func onlyUser(v ssa.Value, user ssa.Instruction) bool {
	for _, ref := range *v.Referrers() {
		if _, debug := ref.(*ssa.DebugRef); !debug && ref != user {
			return false
		}
	}
	return true
}

// borrowUses: вказівник v на місце лише читають, пишуть за ним, беруть
// адресу поля чи елемента з тими самими вживаннями або передають
// параметру, який за зведенням його не зберігає.
func borrowUses(v ssa.Value, ctx *v1Ctx, seen map[ssa.Value]bool) bool {
	if seen[v] {
		return true
	}
	seen[v] = true
	refs := v.Referrers()
	if refs == nil {
		return true
	}
	for _, ref := range *refs {
		switch ref := ref.(type) {
		case *ssa.DebugRef:
		case *ssa.UnOp:
			if ref.Op != token.MUL {
				return false
			}
		case *ssa.Store:
			if ref.Val == v {
				return false
			}
		case *ssa.FieldAddr:
			if !borrowUses(ref, ctx, seen) {
				return false
			}
		case *ssa.IndexAddr:
			if !borrowUses(ref, ctx, seen) {
				return false
			}
		case *ssa.Call:
			if !borrowArg(&ref.Call, v, ctx) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// borrowArg: v — аргумент статичного виклику, параметр якого за зведенням
// не зберігає ні сам вказівник, ні прочитаний з нього.
func borrowArg(call *ssa.CallCommon, v ssa.Value, ctx *v1Ctx) bool {
	callee := call.StaticCallee()
	if callee == nil || call.Value == v {
		return false
	}
	sum := summaryOf(ctx, callee)
	for i, arg := range call.Args {
		if arg != v {
			continue
		}
		idx := summaryIndex(call, i)
		if sum == nil || idx >= len(sum.Params) {
			return false
		}
		p := sum.Params[idx]
		if p.Unknown || p.Side != 0 || p.Content != 0 {
			return false
		}
	}
	return true
}

// cellContent: у комірку cell пишуть лише nil і свіжий результат, єдине
// вживання якого — цей запис, а прочитаний вміст лише порівнюють і
// передають параметру, що лише читається. Тоді каскад може пройти вміст.
func cellContent(cell ssa.Value, ctx *v1Ctx) bool {
	if _, ok := pointee(cell.Type()).Underlying().(*types.Pointer); !ok {
		return false
	}
	for _, ref := range *cell.Referrers() {
		switch ref := ref.(type) {
		case *ssa.DebugRef, *ssa.MakeClosure:
		case *ssa.Store:
			if ref.Addr != cell || !movedInto(ref, ctx) {
				return false
			}
		case *ssa.UnOp:
			if ref.Op != token.MUL || !readOnlyValue(ref, ctx) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// movedInto: store пише nil або свіжий результат виклику, який більше
// ніде не вживається.
func movedInto(store *ssa.Store, ctx *v1Ctx) bool {
	if c, ok := store.Val.(*ssa.Const); ok {
		return c.IsNil()
	}
	call, ok := store.Val.(*ssa.Call)
	if !ok || call.Call.StaticCallee() == nil {
		return false
	}
	sum := summaryOf(ctx, call.Call.StaticCallee())
	return sum != nil && sum.Fresh && !sum.Interior && onlyUser(call, store)
}

func readOnlyValue(v ssa.Value, ctx *v1Ctx) bool {
	for _, ref := range *v.Referrers() {
		switch ref := ref.(type) {
		case *ssa.DebugRef:
		case *ssa.BinOp:
			if ref.Op != token.EQL && ref.Op != token.NEQ {
				return false
			}
		case *ssa.Call:
			if !readOnlyArg(&ref.Call, v, ctx) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// sharedReaders — читачі місця, go замикання, що його захоплює, і
// вживання прочитаного вмісту: каскад звільняє вміст разом з коміркою.
func sharedReaders(site ssa.Instruction, pts map[ssa.Value]siteSet) []ssa.Instruction {
	users := readers(site, pts)
	seen := map[ssa.Instruction]bool{}
	for _, user := range users {
		seen[user] = true
	}
	add := func(refs *[]ssa.Instruction) {
		if refs == nil {
			return
		}
		for _, ref := range *refs {
			if ref != nil && !seen[ref] {
				seen[ref] = true
				users = append(users, ref)
			}
		}
	}
	for _, user := range users {
		switch user := user.(type) {
		case *ssa.MakeClosure:
			add(user.Referrers())
		case *ssa.UnOp:
			add(user.Referrers())
		}
	}
	return users
}

func appendNew(vals []ssa.Value, v ssa.Value) []ssa.Value {
	for _, have := range vals {
		if have == v {
			return vals
		}
	}
	return append(vals, v)
}
