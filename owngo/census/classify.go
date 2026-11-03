package census

import (
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/ssa"
)

// escapeKind — причина втечі в межах однієї функції, від слабшої до сильнішої.
// go, channel і global перекривають closure; return і call — ні.
type escapeKind int

const (
	escapeNone escapeKind = iota
	escapeCall
	escapeReturn
	escapeClosure
	escapeGlobal
	escapeChannel
	escapeGo
)

// transferNote — слово v1, коли втеча прийшла з передачі вказівника.
// Порожнє word лишає ім'я escapeKind: пряма втеча сильніша за move.
type transferNote struct {
	kind escapeKind
	word string
}

func (k escapeKind) name() string {
	switch k {
	case escapeCall:
		return "call"
	case escapeReturn:
		return "return"
	case escapeClosure:
		return "closure"
	case escapeGlobal:
		return "global"
	case escapeChannel:
		return "channel"
	case escapeGo:
		return "go"
	default:
		return ""
	}
}

// siteSet — місця алокації, на які може вказувати значення SSA.
type siteSet map[ssa.Instruction]struct{}

func (s siteSet) add(instr ssa.Instruction) bool {
	if _, ok := s[instr]; ok {
		return false
	}
	s[instr] = struct{}{}
	return true
}

func (s siteSet) absorb(other siteSet) bool {
	changed := false
	for instr := range other {
		if s.add(instr) {
			changed = true
		}
	}
	return changed
}

// messages класифікує місця однієї функції.
// Запис у об'єкт наслідує втечу цього об'єкта; запис у об'єкт, який не втікає, втечею не є.
// Розбір без полів: будь-яке поле ділить одну множину вмісту.
func messages(fn *ssa.Function, mode ifaceMode, universe []types.Type, v1 *v1Ctx) (map[ssa.Instruction]string, map[ssa.Value]siteSet, map[ssa.Instruction]siteSet) {
	var instrs []ssa.Instruction
	for _, block := range fn.Blocks {
		instrs = append(instrs, block.Instrs...)
	}
	fresh := v1.freshSites(instrs)
	pts, contents := pointTo(instrs, fresh)
	if v1 != nil {
		v1.pts = pts
		v1.contents = contents
		linkCallAliases(instrs, pts, v1)
		contents = v1.contents
	}
	pinned := pinnedSites(instrs, pts, v1)
	escape, notes := directEscape(instrs, pts, v1)
	if v1 != nil {
		markAppendAlias(instrs, pts, escape, notes, v1)
	}
	spread(instrs, pts, contents, escape, notes)

	out := make(map[ssa.Instruction]string)
	for _, instr := range instrs {
		if !isSite(instr) && !fresh[instr] {
			continue
		}
		out[instr] = category(instr, escape[instr], notes[instr], pinned[instr], mode, universe, v1 != nil)
	}
	return out, pts, contents
}

// fresh — свіжі виклики v1, які теж є місцями. Їхніх дітей створив
// викликаний код, тож вміст такого місця — воно саме: читання поля дає
// аліас власника, і Dec власника стоїть після останнього вживання дитини.
func pointTo(instrs []ssa.Instruction, fresh map[ssa.Instruction]bool) (map[ssa.Value]siteSet, map[ssa.Instruction]siteSet) {
	pts := map[ssa.Value]siteSet{}
	contents := map[ssa.Instruction]siteSet{}
	for site := range fresh {
		pts[site.(ssa.Value)] = siteSet{site: {}}
		contents[site] = siteSet{site: {}}
	}
	ref := func(v ssa.Value) siteSet {
		set := pts[v]
		if set == nil {
			set = siteSet{}
			pts[v] = set
		}
		return set
	}
	grow := func(v ssa.Value, src siteSet) bool {
		if v == nil || len(src) == 0 {
			return false
		}
		return ref(v).absorb(src)
	}
	hold := func(obj ssa.Instruction, src siteSet) bool {
		if len(src) == 0 {
			return false
		}
		set := contents[obj]
		if set == nil {
			set = siteSet{}
			contents[obj] = set
		}
		return set.absorb(src)
	}

	changed := true
	for changed {
		changed = false
		for _, instr := range instrs {
			// Усі чотири кроки мають виконатися: || обірвав би решту після першого зростання.
			seeded := seed(instr, ref)
			flowed := flow(instr, grow, pts)
			stored := memory(instr, pts, contents, grow, hold)
			aliased := builtinResult(instr, grow, pts)
			if seeded || flowed || stored || aliased {
				changed = true
			}
		}
	}
	return pts, contents
}

func seed(instr ssa.Instruction, ref func(ssa.Value) siteSet) bool {
	switch instr := instr.(type) {
	case *ssa.Alloc:
		return ref(instr).add(instr)
	case *ssa.MakeSlice, *ssa.MakeMap, *ssa.MakeChan:
		return ref(instr.(ssa.Value)).add(instr)
	case *ssa.MakeClosure:
		if len(instr.Bindings) == 0 {
			return false
		}
		return ref(instr).add(instr)
	case *ssa.MakeInterface:
		if directIface(instr.X.Type()) {
			return false
		}
		return ref(instr).add(instr)
	case *ssa.Call, *ssa.BinOp, *ssa.Convert:
		if ImplicitOrigin(instr) == "" {
			return false
		}
		return ref(instr.(ssa.Value)).add(instr)
	default:
		return false
	}
}

func flow(instr ssa.Instruction, grow func(ssa.Value, siteSet) bool, pts map[ssa.Value]siteSet) bool {
	switch instr := instr.(type) {
	case *ssa.Phi:
		changed := false
		for _, edge := range instr.Edges {
			if grow(instr, pts[edge]) {
				changed = true
			}
		}
		return changed
	case *ssa.FieldAddr:
		return grow(instr, pts[instr.X])
	case *ssa.IndexAddr:
		return grow(instr, pts[instr.X])
	case *ssa.Slice:
		return grow(instr, pts[instr.X])
	case *ssa.ChangeType:
		return grow(instr, pts[instr.X])
	case *ssa.Convert:
		if stringSliceConvert(instr.X.Type(), instr.Type()) {
			return false
		}
		if !containsRef(instr.X.Type()) && !containsRef(instr.Type()) {
			return false
		}
		return grow(instr, pts[instr.X])
	case *ssa.MakeInterface:
		return grow(instr, pts[instr.X])
	case *ssa.TypeAssert:
		// Результат — слово даних інтерфейсу або копія з його коробки.
		return grow(instr, pts[instr.X])
	case *ssa.Range:
		// Ітератор — аліас мапи чи рядка: Next читає їх.
		return grow(instr, pts[instr.X])
	case *ssa.Extract:
		return grow(instr, pts[instr.Tuple])
	default:
		return false
	}
}

func memory(instr ssa.Instruction, pts map[ssa.Value]siteSet, contents map[ssa.Instruction]siteSet, grow func(ssa.Value, siteSet) bool, hold func(ssa.Instruction, siteSet) bool) bool {
	switch instr := instr.(type) {
	case *ssa.UnOp:
		if instr.Op != token.MUL || !containsRef(instr.Type()) {
			return false
		}
		carried := siteSet{}
		for obj := range pts[instr.X] {
			carried.absorb(contents[obj])
		}
		return grow(instr, carried)
	case *ssa.Store:
		changed := false
		for obj := range pts[instr.Addr] {
			if hold(obj, pts[instr.Val]) {
				changed = true
			}
		}
		return changed
	case *ssa.MapUpdate:
		changed := false
		for obj := range pts[instr.Map] {
			if hold(obj, pts[instr.Key]) {
				changed = true
			}
			if hold(obj, pts[instr.Value]) {
				changed = true
			}
		}
		return changed
	case *ssa.Lookup:
		if _, ok := instr.X.Type().Underlying().(*types.Map); !ok {
			return false
		}
		return grow(instr, heldBy(pts[instr.X], contents))
	case *ssa.Next:
		if instr.IsString {
			return false
		}
		return grow(instr, heldBy(pts[instr.Iter], contents))
	case *ssa.Call:
		// append копіює елементи другого аргументу в буфер результату.
		if name, ok := builtinName(&instr.Call); !ok || name != "append" || len(instr.Call.Args) < 2 {
			return false
		}
		added := heldBy(pts[instr.Call.Args[1]], contents)
		changed := false
		for obj := range pts[instr] {
			if hold(obj, added) {
				changed = true
			}
		}
		return changed
	default:
		return false
	}
}

// heldBy — вміст усіх об'єктів objs.
func heldBy(objs siteSet, contents map[ssa.Instruction]siteSet) siteSet {
	held := siteSet{}
	for obj := range objs {
		held.absorb(contents[obj])
	}
	return held
}

func builtinResult(instr ssa.Instruction, grow func(ssa.Value, siteSet) bool, pts map[ssa.Value]siteSet) bool {
	call, ok := instr.(*ssa.Call)
	if !ok || len(call.Call.Args) == 0 {
		return false
	}
	name, ok := builtinName(&call.Call)
	if !ok {
		return false
	}
	if !aliasBuiltin(name) {
		return false
	}
	return grow(call, pts[call.Call.Args[0]])
}

// aliasBuiltin: результат вбудованої функції вказує в пам'ять першого
// аргументу.
func aliasBuiltin(name string) bool {
	switch name {
	case "append", "SliceData", "StringData", "String", "Slice":
		return true
	default:
		return false
	}
}

func pinnedSites(instrs []ssa.Instruction, pts map[ssa.Value]siteSet, v1 *v1Ctx) map[ssa.Instruction]bool {
	pinned := map[ssa.Instruction]bool{}
	for _, instr := range instrs {
		// Аргумент параметра, який викликаний перетворює в unsafe.Pointer і
		// зберігає в глобальну змінну чи канал. Перетворення без збереження
		// (race.Acquire у WaitGroup) місце не закріплює: воно лише позичене.
		// Місце з такою втечею й так не звільняється, тож змінюється лише ківш.
		if call, ok := instr.(ssa.CallInstruction); ok && v1 != nil {
			if callee := call.Common().StaticCallee(); callee != nil {
				sum := summaryOf(v1, callee)
				for i, arg := range call.Common().Args {
					idx := summaryIndex(call.Common(), i)
					if sum == nil || idx >= len(sum.Params) || !sum.Params[idx].Pinned {
						continue
					}
					if side := escapeKind(sum.Params[idx].Side); side != escapeGlobal && side != escapeChannel {
						continue
					}
					for site := range pts[arg] {
						pinned[site] = true
					}
				}
			}
		}
		conv, ok := instr.(*ssa.Convert)
		if !ok {
			continue
		}
		basic, ok := conv.Type().Underlying().(*types.Basic)
		if !ok || basic.Kind() != types.UnsafePointer {
			continue
		}
		for site := range pts[conv.X] {
			pinned[site] = true
		}
	}
	return pinned
}

func directEscape(instrs []ssa.Instruction, pts map[ssa.Value]siteSet, v1 *v1Ctx) (map[ssa.Instruction]escapeKind, map[ssa.Instruction]transferNote) {
	escape := map[ssa.Instruction]escapeKind{}
	notes := map[ssa.Instruction]transferNote{}
	for _, instr := range instrs {
		switch instr := instr.(type) {
		case *ssa.Return:
			for _, result := range instr.Results {
				raise(escape, notes, pts[result], escapeReturn, "")
			}
		case *ssa.Send:
			raise(escape, notes, pts[instr.X], escapeChannel, "")
		case *ssa.Select:
			for _, st := range instr.States {
				if st.Dir == types.SendOnly {
					raise(escape, notes, pts[st.Send], escapeChannel, "")
				}
			}
		case *ssa.Store:
			if fromGlobal(instr.Addr) {
				raise(escape, notes, pts[instr.Val], escapeGlobal, "")
			} else if len(pts[instr.Addr]) == 0 {
				// Пам'ять поза місцями функції (поле параметра): значення
				// живе, доки живе вона.
				raise(escape, notes, pts[instr.Val], escapeCall, "")
			}
		case *ssa.MapUpdate:
			if len(pts[instr.Map]) == 0 {
				raise(escape, notes, pts[instr.Key], escapeCall, "")
				raise(escape, notes, pts[instr.Value], escapeCall, "")
			}
		case *ssa.Call:
			noteCall(instr, &instr.Call, escapeCall, pts, escape, notes, false, v1)
		case *ssa.Defer:
			noteCall(instr, &instr.Call, escapeCall, pts, escape, notes, false, v1)
		case *ssa.Go:
			if v1 != nil && v1.skipGo {
				continue
			}
			noteCall(instr, &instr.Call, escapeGo, pts, escape, notes, true, v1)
		case *ssa.Panic:
			// recover отримує значення в будь-якому кадрі вище: як у зведенні, call.
			raise(escape, notes, pts[instr.X], escapeCall, "panic")
		}
	}
	return escape, notes
}

// noteCall піднімає аргументи. Callee лишається, окрім go: там значення функції теж виходить.
// invoke не чіпає отримувача — він callee, не аргумент. builtin не є причиною call.
// go не читає зведення: горутина — інший потік, навіть якщо тіло лише читає вказівник.
// v1 == nil лишає кожен виклик втечею call, як у корпусі v0.
func noteCall(instr ssa.Instruction, call *ssa.CallCommon, k escapeKind, pts map[ssa.Value]siteSet, escape map[ssa.Instruction]escapeKind, notes map[ssa.Instruction]transferNote, includeCallee bool, v1 *v1Ctx) {
	if _, ok := builtinName(call); ok {
		return
	}
	if includeCallee || v1 == nil {
		for _, arg := range call.Args {
			raise(escape, notes, pts[arg], k, "")
		}
		if includeCallee && !call.IsInvoke() {
			raise(escape, notes, pts[call.Value], k, "")
		}
		return
	}
	v1.transfer(instr, call, pts, escape, notes)
}

// raise піднімає місця до k. word записується лише коли вид справді виріс.
// На тому самому виді shared замінює move. Пряма втеча (порожнє word) move не затирає.
func raise(escape map[ssa.Instruction]escapeKind, notes map[ssa.Instruction]transferNote, sites siteSet, k escapeKind, word string) bool {
	changed := false
	for site := range sites {
		if k > escape[site] {
			escape[site] = k
			if word == "" {
				delete(notes, site)
			} else {
				notes[site] = transferNote{kind: k, word: word}
			}
			changed = true
			continue
		}
		if k == escape[site] && word == "shared" {
			if n, ok := notes[site]; ok && n.kind == k && n.word == "move" {
				notes[site] = transferNote{kind: k, word: "shared"}
				changed = true
			}
		}
	}
	return changed
}

func spread(instrs []ssa.Instruction, pts map[ssa.Value]siteSet, contents map[ssa.Instruction]siteSet, escape map[ssa.Instruction]escapeKind, notes map[ssa.Instruction]transferNote) {
	for _, instr := range instrs {
		closure, ok := instr.(*ssa.MakeClosure)
		if !ok {
			continue
		}
		for _, binding := range closure.Bindings {
			raise(escape, notes, pts[binding], escapeClosure, "")
		}
	}
	for {
		grew := false
		for site, k := range escape {
			word := noteWord(notes, site, k)
			for inner := range contents[site] {
				if raise(escape, notes, siteSet{inner: {}}, k, word) {
					grew = true
				}
			}
		}
		for _, instr := range instrs {
			closure, ok := instr.(*ssa.MakeClosure)
			if !ok || escape[closure] < escapeGlobal {
				continue
			}
			k := escape[closure]
			word := noteWord(notes, closure, k)
			for _, binding := range closure.Bindings {
				for site := range pts[binding] {
					if raise(escape, notes, siteSet{site: {}}, k, word) {
						grew = true
					}
				}
			}
		}
		if !grew {
			break
		}
	}
}

func noteWord(notes map[ssa.Instruction]transferNote, site ssa.Instruction, k escapeKind) string {
	n, ok := notes[site]
	if !ok || n.kind != k {
		return ""
	}
	return n.word
}

func category(instr ssa.Instruction, k escapeKind, note transferNote, pinned bool, mode ifaceMode, universe []types.Type, own bool) string {
	origin := ImplicitOrigin(instr)
	// v0 лишає implicit:* . v1 знімає дострокове повернення: володіння — те, що вже доведено.
	if origin != "" && !own {
		return "implicit: " + origin
	}
	// Стек раніше за pinned: змінна з узятою адресою без Heap — це type-pun,
	// з якого компілятор зняв Heap (P11.0.1); купових байтів у неї немає.
	if alloc, ok := instr.(*ssa.Alloc); ok && !alloc.Heap {
		return "stack"
	}
	if pinned {
		return "pinned: unsafe"
	}
	reason := k.name()
	if note.kind == k && note.word != "" {
		reason = note.word
	}
	typ := instr.(ssa.Value).Type()
	if call, ok := instr.(*ssa.Call); ok {
		// Свіжий внутрішній результат звільняється як об'єкт, на який вказує.
		if base := InteriorBase(call); base != nil {
			typ = types.NewPointer(base)
		}
	}
	if cyclic(typ, mode, universe) {
		if k == escapeNone {
			return "cyclic"
		}
		return "cyclic: " + reason
	}
	if k == escapeNone {
		if origin != "" {
			return "static: " + origin
		}
		return "static"
	}
	// return, move, call і panic лишаються в позначці: decSite за ними не ставить Dec.
	if k >= escapeGlobal {
		return "rc-shared: " + reason
	}
	return "rc-local: " + reason
}

// ImplicitOrigin повертає append, concat або string для місця, яке v0
// позначає implicit. Інша інструкція дає порожній рядок.
func ImplicitOrigin(instr ssa.Instruction) string {
	switch instr := instr.(type) {
	case *ssa.Call:
		if b, ok := instr.Call.Value.(*ssa.Builtin); ok && b.Name() == "append" {
			return "append"
		}
	case *ssa.BinOp:
		if instr.Op == token.ADD && isString(instr.X.Type()) {
			return "concat"
		}
	case *ssa.Convert:
		if stringSliceConvert(instr.X.Type(), instr.Type()) {
			return "string"
		}
	}
	return ""
}

// CountableImplicit повідомляє місце append, конкатенації або перетворення
// string і []byte, чий буфер компілятор виділяє сам. []rune сюди не входить.
func CountableImplicit(instr ssa.Instruction) bool {
	switch instr := instr.(type) {
	case *ssa.Call:
		return ImplicitOrigin(instr) == "append"
	case *ssa.BinOp:
		return ImplicitOrigin(instr) == "concat"
	case *ssa.Convert:
		return byteStringConvert(instr.X.Type(), instr.Type())
	default:
		return false
	}
}

func byteStringConvert(from, to types.Type) bool {
	return byteSlice(from) && isString(to) || byteSlice(to) && isString(from)
}

func byteSlice(t types.Type) bool {
	slice, ok := t.Underlying().(*types.Slice)
	if !ok {
		return false
	}
	basic, ok := slice.Elem().Underlying().(*types.Basic)
	return ok && basic.Kind() == types.Byte
}

func builtinName(call *ssa.CallCommon) (string, bool) {
	b, ok := call.Value.(*ssa.Builtin)
	if !ok {
		return "", false
	}
	return b.Name(), true
}

func fromGlobal(v ssa.Value) bool {
	seen := map[ssa.Value]bool{}
	var walk func(ssa.Value) bool
	walk = func(v ssa.Value) bool {
		if v == nil || seen[v] {
			return false
		}
		seen[v] = true
		switch v := v.(type) {
		case *ssa.Global:
			return true
		case *ssa.FieldAddr:
			return walk(v.X)
		case *ssa.IndexAddr:
			return walk(v.X)
		case *ssa.Slice:
			return walk(v.X)
		case *ssa.ChangeType:
			return walk(v.X)
		case *ssa.Convert:
			return walk(v.X)
		case *ssa.Phi:
			return slices.ContainsFunc(v.Edges, walk)
		}
		return false
	}
	return walk(v)
}

// ifaceMode керує ребрами інтерфейсу в графі типів.
// ifaceStrict їх не додає. ifaceConservative веде ребро до кожного
// конкретного типу всесвіту, який можна присвоїти цьому інтерфейсу.
type ifaceMode int

const (
	ifaceStrict ifaceMode = iota
	ifaceConservative
)

// cyclic звітує, чи тип місця після зняття одного вказівника досягає себе.
// Канал, функція і ключ мапи ребра не дають.
func cyclic(t types.Type, mode ifaceMode, universe []types.Type) bool {
	if t == nil {
		return false
	}
	root := t
	if p, ok := t.Underlying().(*types.Pointer); ok {
		root = p.Elem()
	}
	if mode == ifaceConservative {
		universe = slices.Concat(universe, []types.Type{root, types.NewPointer(root)})
	}
	seen := map[types.Type]bool{}
	var walk func(types.Type, int) bool
	walk = func(cur types.Type, depth int) bool {
		if depth > 0 && types.Identical(cur, root) {
			return true
		}
		if seen[cur] {
			return false
		}
		seen[cur] = true
		for _, next := range typeEdges(cur, mode, universe) {
			if walk(next, depth+1) {
				return true
			}
		}
		return false
	}
	return walk(root, 0)
}

func typeEdges(t types.Type, mode ifaceMode, universe []types.Type) []types.Type {
	switch u := t.Underlying().(type) {
	case *types.Pointer:
		return []types.Type{u.Elem()}
	case *types.Slice:
		return []types.Type{u.Elem()}
	case *types.Array:
		return []types.Type{u.Elem()}
	case *types.Map:
		return []types.Type{u.Elem()}
	case *types.Struct:
		out := make([]types.Type, 0, u.NumFields())
		for field := range u.Fields() {
			out = append(out, field.Type())
		}
		return out
	case *types.Interface:
		if mode != ifaceConservative {
			return nil
		}
		var out []types.Type
		for _, cand := range universe {
			if _, ok := cand.Underlying().(*types.Interface); ok {
				continue
			}
			if types.AssignableTo(cand, t) {
				out = append(out, cand)
			}
		}
		return out
	default:
		return nil
	}
}

func containsRef(t types.Type) bool {
	seen := map[types.Type]bool{}
	var walk func(types.Type) bool
	walk = func(t types.Type) bool {
		if t == nil || seen[t] {
			return false
		}
		seen[t] = true
		switch u := t.Underlying().(type) {
		case *types.Pointer, *types.Slice, *types.Map, *types.Chan, *types.Interface, *types.Signature:
			return true
		case *types.Basic:
			return u.Kind() == types.UnsafePointer
		case *types.Struct:
			for field := range u.Fields() {
				if walk(field.Type()) {
					return true
				}
			}
		case *types.Array:
			return walk(u.Elem())
		}
		return false
	}
	return walk(t)
}

// CyclicFields повертає індекси полів структури t, за якими ходить DecCyclic:
// вказівник на структуру, тип якої cyclic у строгому режимі. Зріз, масив,
// мапа й інтерфейс ребра обходу не дають. Тип не структура дає nil.
func CyclicFields(t types.Type) []int {
	if t == nil {
		return nil
	}
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		return nil
	}
	var out []int
	for i := range st.NumFields() {
		ptr, ok := st.Field(i).Type().Underlying().(*types.Pointer)
		if !ok {
			continue
		}
		if _, ok := ptr.Elem().Underlying().(*types.Struct); ok && cyclic(ptr.Elem(), ifaceStrict, nil) {
			out = append(out, i)
		}
	}
	return out
}
