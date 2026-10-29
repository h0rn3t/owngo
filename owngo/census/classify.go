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
func messages(fn *ssa.Function) map[ssa.Instruction]string {
	var instrs []ssa.Instruction
	for _, block := range fn.Blocks {
		instrs = append(instrs, block.Instrs...)
	}
	pts, contents := pointTo(instrs)
	pinned := pinnedSites(instrs, pts)
	escape := directEscape(instrs, pts)
	spread(instrs, pts, contents, escape)

	out := make(map[ssa.Instruction]string)
	for _, instr := range instrs {
		if !isSite(instr) {
			continue
		}
		out[instr] = category(instr, escape[instr], pinned[instr])
	}
	return out
}

func pointTo(instrs []ssa.Instruction) (map[ssa.Value]siteSet, map[ssa.Instruction]siteSet) {
	pts := map[ssa.Value]siteSet{}
	contents := map[ssa.Instruction]siteSet{}
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
		if _, ptr := instr.X.Type().Underlying().(*types.Pointer); ptr {
			return false
		}
		return ref(instr).add(instr)
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
	default:
		return false
	}
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
	if name != "append" && name != "SliceData" && name != "StringData" {
		return false
	}
	return grow(call, pts[call.Call.Args[0]])
}

func pinnedSites(instrs []ssa.Instruction, pts map[ssa.Value]siteSet) map[ssa.Instruction]bool {
	pinned := map[ssa.Instruction]bool{}
	for _, instr := range instrs {
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

func directEscape(instrs []ssa.Instruction, pts map[ssa.Value]siteSet) map[ssa.Instruction]escapeKind {
	escape := map[ssa.Instruction]escapeKind{}
	for _, instr := range instrs {
		switch instr := instr.(type) {
		case *ssa.Return:
			for _, result := range instr.Results {
				raise(escape, pts[result], escapeReturn)
			}
		case *ssa.Send:
			raise(escape, pts[instr.X], escapeChannel)
		case *ssa.Store:
			if fromGlobal(instr.Addr) {
				raise(escape, pts[instr.Val], escapeGlobal)
			}
		case *ssa.Call:
			noteCall(&instr.Call, escapeCall, pts, escape, false)
		case *ssa.Defer:
			noteCall(&instr.Call, escapeCall, pts, escape, false)
		case *ssa.Go:
			noteCall(&instr.Call, escapeGo, pts, escape, true)
		}
	}
	return escape
}

// noteCall піднімає аргументи. Callee лишається, окрім go: там значення функції теж виходить.
// invoke не чіпає отримувача — він callee, не аргумент. builtin не є причиною call.
func noteCall(call *ssa.CallCommon, k escapeKind, pts map[ssa.Value]siteSet, escape map[ssa.Instruction]escapeKind, includeCallee bool) {
	if _, ok := builtinName(call); ok {
		return
	}
	for _, arg := range call.Args {
		raise(escape, pts[arg], k)
	}
	if includeCallee && !call.IsInvoke() {
		raise(escape, pts[call.Value], k)
	}
}

func raise(escape map[ssa.Instruction]escapeKind, sites siteSet, k escapeKind) {
	for site := range sites {
		if k > escape[site] {
			escape[site] = k
		}
	}
}

func spread(instrs []ssa.Instruction, pts map[ssa.Value]siteSet, contents map[ssa.Instruction]siteSet, escape map[ssa.Instruction]escapeKind) {
	for _, instr := range instrs {
		closure, ok := instr.(*ssa.MakeClosure)
		if !ok {
			continue
		}
		for _, binding := range closure.Bindings {
			raise(escape, pts[binding], escapeClosure)
		}
	}
	for {
		grew := false
		for site, k := range escape {
			for inner := range contents[site] {
				if escape[inner] < k {
					escape[inner] = k
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
			for _, binding := range closure.Bindings {
				for site := range pts[binding] {
					if escape[site] < k {
						escape[site] = k
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

func category(instr ssa.Instruction, k escapeKind, pinned bool) string {
	switch instr := instr.(type) {
	case *ssa.Call:
		if b, ok := instr.Call.Value.(*ssa.Builtin); ok && b.Name() == "append" {
			return "implicit: append"
		}
	case *ssa.BinOp:
		if instr.Op == token.ADD && isString(instr.X.Type()) {
			return "implicit: concat"
		}
	case *ssa.Convert:
		if stringSliceConvert(instr.X.Type(), instr.Type()) {
			return "implicit: string"
		}
	}
	if pinned {
		return "pinned: unsafe"
	}
	if alloc, ok := instr.(*ssa.Alloc); ok && !alloc.Heap {
		return "stack"
	}
	if cyclicType(instr.(ssa.Value).Type()) {
		if k == escapeNone {
			return "cyclic"
		}
		return "cyclic: " + k.name()
	}
	if k == escapeNone {
		return "static"
	}
	if k >= escapeGlobal {
		return "rc-shared: " + k.name()
	}
	return "rc-local: " + k.name()
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

// cyclicType звітує, чи тип місця після зняття одного вказівника досягає себе.
// Поле-структура обходиться далі. Поле-інтерфейс ребра не дає; канал за елементом не обходиться.
func cyclicType(t types.Type) bool {
	root := t
	if p, ok := t.Underlying().(*types.Pointer); ok {
		root = p.Elem()
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
		for _, next := range typeEdges(cur) {
			if walk(next, depth+1) {
				return true
			}
		}
		return false
	}
	return walk(root, 0)
}

func typeEdges(t types.Type) []types.Type {
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
