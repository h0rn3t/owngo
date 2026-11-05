package census

import (
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// Sites класифікує кожне місце за правилами v1 (строгий режим інтерфейсів)
// і повертає читачів місць static. Виклик поза funcs невідомий, тож таке
// місце не static. Порожній funcs дає nil, nil.
func Sites(funcs []*ssa.Function) (map[ssa.Instruction]string, map[ssa.Instruction][]ssa.Instruction) {
	cats, uses, _, _, _ := Place(funcs)
	return cats, uses
}

// Place класифікує місця за правилами v1 і збирає дії компілятора.
// staticUses — читачі static, включно з static: append|concat|string.
// decUses — читачі базового купового rc-local, який отримує один Dec.
// counted — ці Alloc і доведений базовий implicit. inc — значення, для яких
// спільна передача у функцію з відомим зведенням ставить Inc. Порожній funcs дає нулі.
func Place(funcs []*ssa.Function) (
	cats map[ssa.Instruction]string,
	staticUses map[ssa.Instruction][]ssa.Instruction,
	decUses map[ssa.Instruction][]ssa.Instruction,
	counted map[ssa.Instruction]struct{},
	inc map[ssa.Instruction][]ssa.Value,
) {
	p := PlaceAll(funcs)
	return p.Cats, p.StaticUses, p.DecUses, p.Counted, p.Inc
}

// Placement — дії компілятора для набору функцій. Поля до Inc — як у Place.
// Shared — місця спільної втечі через go з атомарним словом; їхні читачі
// лежать у DecUses. GoShared — значення, яким go ставить IncShared перед
// собою, а горутина DecShared після повернення своєї функції.
type Placement struct {
	Cats       map[ssa.Instruction]string
	StaticUses map[ssa.Instruction][]ssa.Instruction
	DecUses    map[ssa.Instruction][]ssa.Instruction
	Counted    map[ssa.Instruction]struct{}
	Inc        map[ssa.Instruction][]ssa.Value
	Shared     map[ssa.Instruction]Shared
	GoShared   map[*ssa.Go][]ssa.Value
	// Regions — виклики будівників, які стають коренями регіону (P-NEW-05.2).
	Regions map[ssa.Instruction]struct{}
	// Builders — будівники цих функцій і будівники, яких вони кличуть, зокрема
	// з інших пакетів (P-NEW-05.1): codegen дає першим клон F$region, а в
	// клоні кличе клони других.
	Builders map[*ssa.Function]bool
}

// PlaceAll — Place разом з місцями спільної втечі через go (P9.3).
func PlaceAll(funcs []*ssa.Function) Placement {
	var (
		cats       map[ssa.Instruction]string
		staticUses map[ssa.Instruction][]ssa.Instruction
		decUses    map[ssa.Instruction][]ssa.Instruction
		counted    map[ssa.Instruction]struct{}
		inc        map[ssa.Instruction][]ssa.Value
		shared     map[ssa.Instruction]Shared
		goShared   map[*ssa.Go][]ssa.Value
		regions    map[ssa.Instruction]struct{}
		builders   map[*ssa.Function]bool
	)
	if len(funcs) == 0 {
		return Placement{}
	}
	ctx := &v1Ctx{sums: packageSummaries(nil, funcs)}
	cats = map[ssa.Instruction]string{}
	staticUses = map[ssa.Instruction][]ssa.Instruction{}
	decUses = map[ssa.Instruction][]ssa.Instruction{}
	counted = map[ssa.Instruction]struct{}{}
	storeInc := map[ssa.Instruction][]ssa.Value{}
	for _, fn := range funcs {
		if fn == nil || len(fn.Blocks) == 0 {
			continue
		}
		if sum := ctx.sums[fn]; sum != nil && sum.Builder {
			if builders == nil {
				builders = map[*ssa.Function]bool{}
			}
			builders[fn] = true
			for _, block := range fn.Blocks {
				for _, instr := range block.Instrs {
					call, ok := instr.(*ssa.Call)
					if !ok || call.Call.StaticCallee() == nil {
						continue
					}
					if sum := summaryOf(ctx, call.Call.StaticCallee()); sum != nil && sum.Builder {
						builders[call.Call.StaticCallee()] = true
					}
				}
			}
		}
		msg, pts, contents := messages(fn, ifaceStrict, nil, ctx)
		ctx.blockDec(exitSites(fn, pts, contents))
		// Свіжий результат: одиниця слова переходить до викликача.
		if sum := ctx.sums[fn]; sum != nil && sum.Fresh {
			for _, alloc := range freshAllocs(fn) {
				counted[alloc] = struct{}{}
			}
		}
		sites, goVals, sharedUses := sharedSites(fn, msg, pts, contents, ctx)
		for site, info := range sites {
			if shared == nil {
				shared = map[ssa.Instruction]Shared{}
				goShared = map[*ssa.Go][]ssa.Value{}
			}
			shared[site] = info
			counted[site] = struct{}{}
			if users := sharedUses[site]; len(users) > 0 {
				decUses[site] = users
			}
		}
		for g, vals := range goVals {
			goShared[g] = vals
		}
		rings := cyclicSites(fn, msg, pts, storeInc, ctx)
		for site := range rings {
			counted[site] = struct{}{}
			if ctx.noDec[site] {
				continue
			}
			if users := rcUsers(siteUsers(site, pts, contents), msg, pts); len(users) > 0 {
				decUses[site] = users
			}
		}
		for site, label := range msg {
			cats[site] = label
			if regionRoot(site, ctx) {
				if regions == nil {
					regions = map[ssa.Instruction]struct{}{}
				}
				regions[site] = struct{}{}
			}
			head, _, _ := strings.Cut(label, ":")
			if head == "static" && freshSite(site) {
				// Слово вже є: замість FreeAccounted — DecAccounted.
				counted[site] = struct{}{}
				if users := siteUsers(site, pts, contents); len(users) > 0 && !ctx.noDec[site] {
					decUses[site] = users
				}
				continue
			}
			if head == "static" {
				users := siteUsers(site, pts, contents)
				if len(users) > 0 {
					staticUses[site] = users
				}
				continue
			}
			// Внутрішній cyclic не входить у кільце: його вказівник не початок
			// вузла. Dec на ньому — DecCyclic з дескриптором бази.
			cyclicInterior := label == "cyclic" && interiorCall(site)
			if !countedAlloc(site, label) && !countedImplicit(site, label) && !cyclicInterior {
				continue
			}
			// Слово без Dec нічого не дає, а вказівник, що виходить із функції,
			// мусить лишатися початком блока malloc.
			if !decSite(site, label, pts, ctx) {
				continue
			}
			counted[site] = struct{}{}
			users := rcUsers(siteUsers(site, pts, contents), msg, pts)
			if len(users) > 0 {
				decUses[site] = users
			}
		}
	}
	inc = filterInc(ctx.inc, counted)
	for store, vals := range storeInc {
		if inc == nil {
			inc = map[ssa.Instruction][]ssa.Value{}
		}
		inc[store] = vals
	}
	if len(cats) == 0 {
		cats = nil
	}
	if len(staticUses) == 0 {
		staticUses = nil
	}
	if len(decUses) == 0 {
		decUses = nil
	}
	if len(counted) == 0 {
		counted = nil
	}
	return Placement{
		Cats:       cats,
		StaticUses: staticUses,
		DecUses:    decUses,
		Counted:    counted,
		Inc:        inc,
		Shared:     shared,
		GoShared:   goShared,
		Regions:    regions,
		Builders:   builders,
	}
}

// interiorCall: місце — виклик зі свіжим внутрішнім результатом.
func interiorCall(site ssa.Instruction) bool {
	call, ok := site.(*ssa.Call)
	return ok && freshSite(site) && InteriorBase(call) != nil
}

func countedAlloc(site ssa.Instruction, label string) bool {
	alloc, ok := site.(*ssa.Alloc)
	if (!ok || !alloc.Heap) && !freshSite(site) {
		return false
	}
	head, _, _ := strings.Cut(label, ":")
	return head == "rc-local"
}

func decSite(site ssa.Instruction, label string, pts map[ssa.Value]siteSet, v1 *v1Ctx) bool {
	if v1.noDec[site] || interiorEscapes(site, pts, v1) {
		return false
	}
	return !leaves(label)
}

// leaves звітує, що місце з позначкою label іде з функції: return, move, call
// і panic віддають його іншому власнику, Dec тут не ставиться.
func leaves(label string) bool {
	_, reason, _ := strings.Cut(label, ": ")
	switch reason {
	case "return", "move", "call", "panic":
		return true
	default:
		return false
	}
}

// exitSites повертає місця, які return чи panic віддають за межі функції,
// разом з їхнім вмістом. У позначці closure сильніша за return і panic,
// тож decSite за нею цих місць не бачить.
func exitSites(fn *ssa.Function, pts map[ssa.Value]siteSet, contents map[ssa.Instruction]siteSet) siteSet {
	out := siteSet{}
	var add func(siteSet)
	add = func(sites siteSet) {
		for site := range sites {
			if out.add(site) {
				add(contents[site])
			}
		}
	}
	for _, block := range fn.Blocks {
		switch instr := block.Instrs[len(block.Instrs)-1].(type) {
		case *ssa.Return:
			for _, result := range instr.Results {
				add(pts[result])
			}
		case *ssa.Panic:
			add(pts[instr.X])
		}
	}
	return out
}

func countedImplicit(site ssa.Instruction, label string) bool {
	if !CountableImplicit(site) {
		return false
	}
	head, _, _ := strings.Cut(label, ":")
	return head == "rc-local"
}

// siteUsers додає читачів кожного контейнера, у contents якого лежить це
// місце, транзитивно: виклик, що читає контейнер, читає і місце в ньому
// (зріз у v.e, v у s, read(s)). Замикання тримає alloc змінної, а не саму
// конкатенацію: без цього Dec став би на Store і звільнив буфер до виклику
// замикання.
func siteUsers(site ssa.Instruction, pts map[ssa.Value]siteSet, contents map[ssa.Instruction]siteSet) []ssa.Instruction {
	users := readers(site, pts)
	holders := siteSet{site: {}}
	for grew := true; grew; {
		grew = false
		for obj, inner := range contents {
			if _, done := holders[obj]; done || !overlaps(inner, holders) {
				continue
			}
			holders.add(obj)
			users = append(users, readers(obj, pts)...)
			grew = true
		}
	}
	return users
}

func rcUsers(users []ssa.Instruction, msg map[ssa.Instruction]string, pts map[ssa.Value]siteSet) []ssa.Instruction {
	extra, gone := closureUses(users, msg, pts)
	if gone {
		return nil
	}
	if len(extra) == 0 {
		return users
	}
	return append(users, extra...)
}

// closureUses: замикання, що тримає місце, і є його вживанням разом з усіма
// читачами значень, що на нього вказують (і через локальний масив чи
// структуру). Замикання, що йде з функції (повернуте, у горутину, канал,
// глобальну змінну чи за позначкою leaves — у поле параметра, у виклик),
// забирає місце з собою.
func closureUses(users []ssa.Instruction, msg map[ssa.Instruction]string, pts map[ssa.Value]siteSet) (extra []ssa.Instruction, gone bool) {
	seen := map[ssa.Instruction]bool{}
	for _, user := range users {
		seen[user] = true
	}
	for _, user := range users {
		clo, ok := user.(*ssa.MakeClosure)
		if !ok {
			continue
		}
		if leaves(msg[clo]) {
			return nil, true
		}
		for _, ref := range readers(clo, pts) {
			switch ref := ref.(type) {
			case *ssa.Return, *ssa.Go, *ssa.Send, *ssa.Select:
				return nil, true
			case *ssa.Store:
				if fromGlobal(ref.Addr) {
					return nil, true
				}
			}
			if seen[ref] {
				continue
			}
			seen[ref] = true
			extra = append(extra, ref)
		}
	}
	return extra, false
}

// interiorEscapes звітує, що похідний вказівник на місце може пережити
// функцію або вказати не на нього. Dec стоїть на самому місці після
// останнього читача, а читачі включають вживання похідних (readers), тож
// похідний вказівник, що лишається в функції бази, місця не блокує.
func interiorEscapes(site ssa.Instruction, pts map[ssa.Value]siteSet, v1 *v1Ctx) bool {
	for val, set := range pts {
		if _, ok := set[site]; !ok || !interiorPointer(val) {
			continue
		}
		if !ownBase(val, site, pts) || !localInterior(val, v1) {
			return true
		}
	}
	return false
}

// ownBase звітує, що ланцюг адрес val починається зі значення функції
// місця, яке вказує лише на це місце: саме місце чи читання змінної,
// захопленої замиканням.
func ownBase(val ssa.Value, site ssa.Instruction, pts map[ssa.Value]siteSet) bool {
	base := val
	for {
		switch x := base.(type) {
		case *ssa.FieldAddr:
			base = x.X
		case *ssa.IndexAddr:
			base = x.X
		case *ssa.Slice:
			base = x.X
		case *ssa.ChangeType:
			base = x.X
		case *ssa.Convert:
			base = x.X
		default:
			set := pts[base]
			_, ok := set[site]
			return base.Parent() == site.Parent() && len(set) == 1 && ok
		}
	}
}

// localInterior звітує, що val лише читають, пишуть за ним, порівнюють,
// беруть від нього похідний вказівник (той перевіряється окремо) і
// передають параметру, який лише читається.
func localInterior(val ssa.Value, v1 *v1Ctx) bool {
	refs := val.Referrers()
	if refs == nil {
		return true
	}
	for _, ref := range *refs {
		switch ref := ref.(type) {
		case *ssa.DebugRef, *ssa.FieldAddr, *ssa.IndexAddr, *ssa.Slice, *ssa.ChangeType, *ssa.Convert:
		case *ssa.UnOp:
			if ref.Op != token.MUL {
				return false
			}
		case *ssa.Store:
			if ref.Val == val {
				return false
			}
		case *ssa.BinOp:
			if ref.Op != token.EQL && ref.Op != token.NEQ {
				return false
			}
		case *ssa.Call:
			if name, ok := builtinName(&ref.Call); ok {
				if name != "len" && name != "cap" && name != "copy" {
					return false
				}
				continue
			}
			if !readOnlyArg(&ref.Call, val, v1) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func interiorPointer(v ssa.Value) bool {
	seen := map[ssa.Value]bool{}
	for v != nil && !seen[v] {
		seen[v] = true
		switch x := v.(type) {
		case *ssa.FieldAddr, *ssa.IndexAddr, *ssa.Slice:
			return true
		case *ssa.ChangeType:
			v = x.X
		case *ssa.Convert:
			v = x.X
		default:
			return false
		}
	}
	return false
}

func filterInc(raw map[ssa.Instruction][]ssa.Value, counted map[ssa.Instruction]struct{}) map[ssa.Instruction][]ssa.Value {
	if len(raw) == 0 || len(counted) == 0 {
		return nil
	}
	out := map[ssa.Instruction][]ssa.Value{}
	for call, vals := range raw {
		seen := map[ssa.Value]bool{}
		var keep []ssa.Value
		for _, val := range vals {
			instr, ok := val.(ssa.Instruction)
			if !ok || seen[val] {
				continue
			}
			if _, ok := counted[instr]; !ok {
				continue
			}
			seen[val] = true
			keep = append(keep, val)
		}
		if len(keep) > 0 {
			out[call] = keep
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// StaticUses повідомляє місця категорії static за правилами v1 і інструкції,
// які їх читають. Виклик функції поза funcs невідомий, тож таке місце не static.
// Порожній funcs дає nil.
func StaticUses(funcs []*ssa.Function) map[ssa.Instruction][]ssa.Instruction {
	_, uses := Sites(funcs)
	return uses
}

func readers(site ssa.Instruction, pts map[ssa.Value]siteSet) []ssa.Instruction {
	seen := map[ssa.Instruction]struct{}{}
	var users []ssa.Instruction
	for val, set := range pts {
		if _, ok := set[site]; !ok || val == nil || val.Referrers() == nil {
			continue
		}
		for _, ref := range *val.Referrers() {
			if ref == nil || ref == site {
				continue
			}
			if _, ok := seen[ref]; ok {
				continue
			}
			seen[ref] = struct{}{}
			users = append(users, ref)
		}
	}
	return users
}

// cyclicSites повертає купові місця з позначкою рівно cyclic, яким DecCyclic
// безпечний, і додає до storeInc запис їхнього вказівника в поле обходу.
// DecCyclic читає слово перед ціллю кожного поля з CyclicFields, тож у таке
// поле місця потрапляє лише nil або вказівник іншого такого місця. Значення,
// що вказує на місце, вживається лише в доступі до поля, записі в поле
// обходу, порівнянні й phi; інакше місце, а за ним і місця, що його
// записали, лічильника не отримують.
func cyclicSites(fn *ssa.Function, msg map[ssa.Instruction]string, pts map[ssa.Value]siteSet, storeInc map[ssa.Instruction][]ssa.Value, v1 *v1Ctx) siteSet {
	cand := siteSet{}
	for site, label := range msg {
		alloc, ok := site.(*ssa.Alloc)
		if (!ok || !alloc.Heap) && !freshSite(site) || label != "cyclic" || interiorCall(site) {
			continue
		}
		if len(CyclicFields(pointee(site.(ssa.Value).Type()))) > 0 {
			cand.add(site)
		}
	}
	for changed := len(cand) > 0; changed; {
		changed = false
		for v, sites := range pts {
			// Адресу поля перевіряє fieldUses через її базове значення.
			if _, ok := v.(*ssa.FieldAddr); ok || v.Parent() != fn || !overlaps(sites, cand) || ringUses(v, cand, v1) {
				continue
			}
			for site := range sites {
				if _, ok := cand[site]; ok {
					delete(cand, site)
					changed = true
				}
			}
		}
	}
	if len(cand) == 0 {
		return nil
	}
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			store, ok := instr.(*ssa.Store)
			if !ok || !ringField(store.Addr) {
				continue
			}
			if c, ok := store.Val.(*ssa.Const); ok && c.IsNil() {
				continue
			}
			if overlaps(pts[store.Val], cand) {
				storeInc[store] = []ssa.Value{store.Val}
			}
		}
	}
	return cand
}

func overlaps(a, b siteSet) bool {
	for site := range a {
		if _, ok := b[site]; ok {
			return true
		}
	}
	return false
}

// ringUses звітує, що кожне вживання v тримає його всередині кільця.
// Виклик дозволений, якщо параметр за зведенням лише читається.
func ringUses(v ssa.Value, cand siteSet, v1 *v1Ctx) bool {
	refs := v.Referrers()
	if refs == nil {
		return true
	}
	for _, ref := range *refs {
		switch ref := ref.(type) {
		case *ssa.Phi, *ssa.DebugRef:
		case *ssa.BinOp:
			if ref.Op != token.EQL && ref.Op != token.NEQ {
				return false
			}
		case *ssa.Call:
			if !readOnlyArg(&ref.Call, v, v1) {
				return false
			}
		case *ssa.FieldAddr:
			if !fieldUses(ref, cand) {
				return false
			}
		case *ssa.Store:
			if ref.Addr == v {
				// Запис цілого значення копіює поле без Inc; лишається нуль.
				if _, ok := ref.Val.(*ssa.Const); !ok {
					return false
				}
				continue
			}
			addr, ok := ref.Addr.(*ssa.FieldAddr)
			if !ok || !ringField(addr) || !countedRef(addr.X, cand, map[ssa.Value]bool{}) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// fieldUses звітує, що адресу поля лише читають і в неї лише пишуть,
// і що в поле обходу пишуть nil або вказівник місця з cand.
func fieldUses(addr *ssa.FieldAddr, cand siteSet) bool {
	refs := addr.Referrers()
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
			if ref.Addr != addr {
				return false
			}
			if ringField(addr) && !countedRef(ref.Val, cand, map[ssa.Value]bool{}) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// ringField звітує, що addr — поле з CyclicFields.
func ringField(addr ssa.Value) bool {
	fa, ok := addr.(*ssa.FieldAddr)
	if !ok {
		return false
	}
	return slices.Contains(CyclicFields(pointee(fa.X.Type())), fa.Field)
}

// countedRef звітує, що v — nil, місце з cand, phi таких значень або
// читання поля обходу з такого значення: за v лежить слово AllocCounted.
func countedRef(v ssa.Value, cand siteSet, seen map[ssa.Value]bool) bool {
	if seen[v] {
		return true
	}
	seen[v] = true
	switch v := v.(type) {
	case *ssa.Const:
		return v.IsNil()
	case *ssa.Alloc, *ssa.Call:
		_, ok := cand[v.(ssa.Instruction)]
		return ok
	case *ssa.Phi:
		for _, edge := range v.Edges {
			if !countedRef(edge, cand, seen) {
				return false
			}
		}
		return true
	case *ssa.UnOp:
		addr, ok := v.X.(*ssa.FieldAddr)
		return ok && v.Op == token.MUL && ringField(addr) && countedRef(addr.X, cand, seen)
	default:
		return false
	}
}

// pointee повертає тип за вказівником або nil.
func pointee(t types.Type) types.Type {
	if p, ok := t.Underlying().(*types.Pointer); ok {
		return p.Elem()
	}
	return nil
}

// readOnlyArg: v — аргумент статичного виклику з відомим зведенням, де
// параметр не втікає, і функція в його пам'ять не пише.
func readOnlyArg(call *ssa.CallCommon, v ssa.Value, v1 *v1Ctx) bool {
	callee := call.StaticCallee()
	if callee == nil || call.Value == v || v1 == nil {
		return false
	}
	sum := summaryOf(v1, callee)
	for i, arg := range call.Args {
		if arg != v {
			continue
		}
		idx := summaryIndex(call, i)
		if sum == nil || idx >= len(sum.Params) {
			return false
		}
		p := sum.Params[idx]
		if p.Unknown || p.Side != 0 || p.Content != 0 || p.Writes {
			return false
		}
	}
	return true
}
