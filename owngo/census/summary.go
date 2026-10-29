package census

import (
	"go/token"
	"go/types"
	"slices"
	"sync"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ssa"
)

// Summary — зведення параметрів однієї функції для класифікації v1.
// Індекс 0 — отримувач, якщо сигнатура його має. Факти йдуть між пакетами.
type Summary struct {
	Params []Param
}

// Param описує один параметр після міжпроцедурного розбору.
// Unknown означає невідомі цілі або відсутнє тіло: це ніколи не позичання.
// Side — код escapeKind для втечі самого вказівника; 0 означає, що параметр його не зберігає.
// Content — те саме для вказівника, зчитаного зсередини параметра.
// Alias — індекси результатів, у які вказівник протікає.
type Param struct {
	Unknown bool
	Side    uint8
	Content uint8
	Alias   []int
}

// AFact позначає Summary фактом go/analysis.
func (*Summary) AFact() {}

// v1Ctx — зведення пакета і факти імпорту для одного проходу аналізатора.
type v1Ctx struct {
	sums     map[*ssa.Function]*Summary
	pass     *analysis.Pass
	pts      map[ssa.Value]siteSet
	contents map[ssa.Instruction]siteSet
	seen     map[*ssa.Function]resolved
}

// resolved — замкнені конкретні типи й функції однієї SSA-функції.
// localFacts дорогий, тож пакет рахує його один раз на функцію.
type resolved struct {
	concrete map[ssa.Value][]types.Type
	fns      map[ssa.Value][]*ssa.Function
}

type pset map[int]struct{}

func packageSummaries(pass *analysis.Pass, funcs []*ssa.Function) map[*ssa.Function]*Summary {
	cur := make(map[*ssa.Function]*Summary, len(funcs))
	for _, fn := range funcs {
		if fn == nil || fn.Blocks == nil {
			cur[fn] = unknownSummary(fn)
			continue
		}
		cur[fn] = &Summary{Params: make([]Param, len(fn.Params))}
	}
	cache := map[*ssa.Function]resolved{}
	callers := map[*ssa.Function][]*ssa.Function{}
	for _, fn := range funcs {
		if fn == nil || fn.Blocks == nil {
			continue
		}
		for _, cal := range calledFns(fn, cache) {
			callers[cal] = append(callers[cal], fn)
		}
	}
	lookup := func(fn *ssa.Function) *Summary {
		if s, ok := cur[fn]; ok {
			return s
		}
		return importSummary(pass, fn)
	}
	queue := make([]*ssa.Function, 0, len(funcs))
	queued := map[*ssa.Function]bool{}
	push := func(fn *ssa.Function) {
		if fn == nil || fn.Blocks == nil || queued[fn] {
			return
		}
		queued[fn] = true
		queue = append(queue, fn)
	}
	for _, fn := range funcs {
		push(fn)
	}
	limit := len(funcs)*len(funcs) + 8
	steps := 0
	for head := 0; head < len(queue); head++ {
		fn := queue[head]
		queued[fn] = false
		steps++
		if steps > limit {
			for _, item := range funcs {
				cur[item] = unknownSummary(item)
			}
			break
		}
		next := summarize(fn, lookup, cache)
		if paramsEqual(cur[fn].Params, next.Params) {
			continue
		}
		cur[fn] = next
		for _, caller := range callers[fn] {
			push(caller)
		}
	}
	for _, fn := range funcs {
		if fn == nil || fn.Object() == nil || cur[fn] == nil {
			continue
		}
		obj := fn.Object()
		if !obj.Exported() {
			continue
		}
		publishSummary(obj, cur[fn])
		pass.ExportObjectFact(obj, cur[fn])
	}
	return cur
}

// summaryBy тримає зведення експортованих функцій між окремими проходами
// аналізатора. phase0 класифікує пакети модуля по одному: FactTypes на весь
// граф імпорту будує SSA залежностей і не вміщається в пам'ять.
var (
	summaryMu sync.Mutex
	summaryBy = map[string]*Summary{}
)

// ResetSummaries забуває зведення попереднього проходу phase0.
func ResetSummaries() {
	summaryMu.Lock()
	summaryBy = map[string]*Summary{}
	summaryMu.Unlock()
}

func publishSummary(obj types.Object, sum *Summary) {
	key := summaryKey(obj)
	if key == "" || sum == nil {
		return
	}
	summaryMu.Lock()
	summaryBy[key] = sum
	summaryMu.Unlock()
}

func storedSummary(obj types.Object) *Summary {
	key := summaryKey(obj)
	if key == "" {
		return nil
	}
	summaryMu.Lock()
	defer summaryMu.Unlock()
	return summaryBy[key]
}

func summaryKey(obj types.Object) string {
	if obj == nil || obj.Pkg() == nil {
		return ""
	}
	return obj.Pkg().Path() + " " + types.ObjectString(obj, nil)
}

// summaryIndex — індекс Param для аргумента виклику.
// У invoke отримувач не входить у Args: він уже в слоті 0 зведення.
func summaryIndex(call *ssa.CallCommon, arg int) int {
	if call.IsInvoke() {
		return arg + 1
	}
	return arg
}

func importSummary(pass *analysis.Pass, fn *ssa.Function) *Summary {
	if fn != nil && fn.Object() != nil {
		var got Summary
		if pass != nil && pass.ImportObjectFact(fn.Object(), &got) {
			return &got
		}
		if stored := storedSummary(fn.Object()); stored != nil {
			return stored
		}
	}
	return unknownSummary(fn)
}

func unknownSummary(fn *ssa.Function) *Summary {
	n := 0
	if fn != nil {
		n = len(fn.Params)
		if n == 0 && fn.Signature != nil {
			n = fn.Signature.Params().Len()
			if fn.Signature.Recv() != nil {
				n++
			}
		}
	}
	s := &Summary{Params: make([]Param, n)}
	for i := range s.Params {
		s.Params[i].Unknown = true
	}
	return s
}

func paramsEqual(a, b []Param) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Unknown != b[i].Unknown || a[i].Side != b[i].Side || a[i].Content != b[i].Content || !slices.Equal(a[i].Alias, b[i].Alias) {
			return false
		}
	}
	return true
}

func calledFns(fn *ssa.Function, cache map[*ssa.Function]resolved) []*ssa.Function {
	instrs := instructions(fn)
	var dynamic []ssa.CallInstruction
	var out []*ssa.Function
	for _, instr := range instrs {
		call, ok := instr.(ssa.CallInstruction)
		if !ok {
			continue
		}
		common := call.Common()
		if _, builtin := builtinName(common); builtin {
			continue
		}
		if target := common.StaticCallee(); target != nil {
			out = append(out, target)
			continue
		}
		dynamic = append(dynamic, call)
	}
	if len(dynamic) == 0 {
		return out
	}
	got := remember(cache, fn)
	for _, call := range dynamic {
		targets, ok := callTargets(call.Common(), got.concrete, got.fns, fn.Prog)
		if !ok {
			continue
		}
		out = append(out, targets...)
	}
	return out
}

func dynamicCall(instrs []ssa.Instruction) bool {
	for _, instr := range instrs {
		call, ok := instr.(ssa.CallInstruction)
		if !ok {
			continue
		}
		common := call.Common()
		if _, builtin := builtinName(common); builtin {
			continue
		}
		if common.StaticCallee() == nil {
			return true
		}
	}
	return false
}

func remember(cache map[*ssa.Function]resolved, fn *ssa.Function) resolved {
	if fn == nil {
		return resolved{}
	}
	if got, ok := cache[fn]; ok {
		return got
	}
	concrete, fns := localFacts(instructions(fn))
	got := resolved{concrete: concrete, fns: fns}
	cache[fn] = got
	return got
}

func instructions(fn *ssa.Function) []ssa.Instruction {
	var instrs []ssa.Instruction
	for _, block := range fn.Blocks {
		instrs = append(instrs, block.Instrs...)
	}
	return instrs
}

func summarize(fn *ssa.Function, lookup func(*ssa.Function) *Summary, cache map[*ssa.Function]resolved) *Summary {
	sum := &Summary{Params: make([]Param, len(fn.Params))}
	if fn.Blocks == nil {
		return unknownSummary(fn)
	}
	instrs := instructions(fn)
	var concrete map[ssa.Value][]types.Type
	var knownFn map[ssa.Value][]*ssa.Function
	if dynamicCall(instrs) {
		got := remember(cache, fn)
		concrete, knownFn = got.concrete, got.fns
	}
	tok := map[ssa.Value]pset{}
	derived := map[ssa.Value]pset{}
	content := map[ssa.Value]pset{}
	tuple := map[*ssa.Call][]pset{}
	for i, param := range fn.Params {
		if containsRef(param.Type()) {
			tok[param] = pset{i: {}}
		}
	}
	limit := len(instrs)*max(len(fn.Params), 1) + 2
	hit := true
	for range limit {
		changed := false
		for _, instr := range instrs {
			if applySummary(instr, sum, tok, derived, content, tuple, lookup, concrete, knownFn, fn.Prog) {
				changed = true
			}
		}
		if !changed {
			hit = false
			break
		}
	}
	// Нерухомої точки немає: оптимістичне позичання було б хибним.
	if hit {
		return unknownSummary(fn)
	}
	return sum
}

func applySummary(instr ssa.Instruction, sum *Summary, tok, derived, content map[ssa.Value]pset, tuple map[*ssa.Call][]pset, lookup func(*ssa.Function) *Summary, concrete map[ssa.Value][]types.Type, knownFn map[ssa.Value][]*ssa.Function, prog *ssa.Program) bool {
	switch instr := instr.(type) {
	case *ssa.Go:
		return sideCall(sum, instr.Common(), escapeGo, tok, derived, content)
	case *ssa.Call:
		return applyCall(instr, &instr.Call, sum, tok, derived, content, tuple, lookup, concrete, knownFn, prog)
	case *ssa.Defer:
		return applyCall(instr, &instr.Call, sum, tok, derived, content, tuple, lookup, concrete, knownFn, prog)
	case *ssa.Return:
		changed := false
		for i, res := range instr.Results {
			changed = sum.addAlias(tok[res], i) || changed
			changed = sum.addAlias(derived[res], i) || changed
			changed = sum.addAlias(content[res], i) || changed
		}
		return changed
	case *ssa.Store:
		extra := mergeSets(tok[instr.Val], derived[instr.Val])
		changed := unionAt(content, instr.Addr, extra)
		if base := addressBase(instr.Addr); base != instr.Addr {
			changed = unionAt(content, base, extra) || changed
		}
		if fromGlobal(instr.Addr) {
			changed = sum.raiseSide(tok[instr.Val], escapeGlobal, false) || changed
			changed = sum.raiseSide(derived[instr.Val], escapeGlobal, true) || changed
			changed = sum.raiseSide(content[instr.Val], escapeGlobal, false) || changed
		}
		return changed
	case *ssa.Send:
		changed := sum.raiseSide(tok[instr.X], escapeChannel, false)
		changed = sum.raiseSide(derived[instr.X], escapeChannel, true) || changed
		return sum.raiseSide(content[instr.X], escapeChannel, false) || changed
	case *ssa.MapUpdate:
		extra := mergeSets(mergeSets(tok[instr.Value], derived[instr.Value]), mergeSets(tok[instr.Key], derived[instr.Key]))
		changed := unionAt(content, instr.Map, extra)
		if base := addressBase(instr.Map); base != instr.Map {
			changed = unionAt(content, base, extra) || changed
		}
		if fromGlobal(instr.Map) {
			changed = sum.raiseSide(extra, escapeGlobal, false) || changed
		}
		return changed
	case *ssa.Panic:
		changed := sum.raiseSide(tok[instr.X], escapeCall, false)
		return sum.raiseSide(derived[instr.X], escapeCall, true) || changed
	case *ssa.UnOp:
		if instr.Op == token.MUL || instr.Op == token.ARROW {
			changed := unionAt(derived, instr, tok[instr.X])
			changed = unionAt(derived, instr, derived[instr.X]) || changed
			return unionAt(derived, instr, content[instr.X]) || changed
		}
		changed := unionAt(tok, instr, tok[instr.X])
		return unionAt(derived, instr, derived[instr.X]) || changed
	case *ssa.Extract:
		call, ok := instr.Tuple.(*ssa.Call)
		if !ok || instr.Index >= len(tuple[call]) {
			return false
		}
		return unionAt(tok, instr, tuple[call][instr.Index])
	default:
		value, ok := instr.(ssa.Value)
		if !ok {
			return false
		}
		t, d := operandSets(instr, tok, derived)
		changed := unionAt(tok, value, t)
		return unionAt(derived, value, d) || changed
	}
}

func applyCall(instr ssa.Instruction, call *ssa.CallCommon, sum *Summary, tok, derived, content map[ssa.Value]pset, tuple map[*ssa.Call][]pset, lookup func(*ssa.Function) *Summary, concrete map[ssa.Value][]types.Type, knownFn map[ssa.Value][]*ssa.Function, prog *ssa.Program) bool {
	if name, ok := builtinName(call); ok {
		if name == "append" && len(call.Args) > 0 {
			value, ok := instr.(ssa.Value)
			if !ok {
				return false
			}
			changed := unionAt(tok, value, tok[call.Args[0]])
			return unionAt(derived, value, derived[call.Args[0]]) || changed
		}
		return false
	}
	targets, known := callTargets(call, concrete, knownFn, prog)
	nres := resultCount(call)
	if !known {
		return unknownArgs(sum, call, tok, derived, content)
	}
	changed := false
	flow := make([]pset, nres)
	for i, arg := range call.Args {
		ps, ok := mergedParam(targets, summaryIndex(call, i), lookup)
		if !ok || ps.Unknown {
			changed = sum.markUnknown(tok[arg]) || changed
			changed = sum.markUnknown(derived[arg]) || changed
			changed = sum.markUnknown(content[arg]) || changed
			continue
		}
		changed = sum.raiseSide(tok[arg], escapeKind(ps.Side), false) || changed
		changed = sum.raiseSide(derived[arg], escapeKind(ps.Side), true) || changed
		changed = sum.raiseSide(tok[arg], escapeKind(ps.Content), true) || changed
		if ps.Side != 0 {
			changed = sum.raiseSide(content[arg], escapeKind(ps.Side), false) || changed
		}
		for _, ri := range ps.Alias {
			if ri < 0 || ri >= nres {
				continue
			}
			flow[ri] = mergeSets(flow[ri], tok[arg])
			flow[ri] = mergeSets(flow[ri], derived[arg])
		}
	}
	if value, ok := instr.(ssa.Value); ok && nres == 1 {
		changed = unionAt(tok, value, flow[0]) || changed
	}
	if callValue, ok := instr.(*ssa.Call); ok && nres > 1 {
		prev := tuple[callValue]
		if !flowsEqual(prev, flow) {
			tuple[callValue] = flow
			changed = true
		}
	}
	return changed
}

func sideCall(sum *Summary, call *ssa.CallCommon, k escapeKind, tok, derived, content map[ssa.Value]pset) bool {
	changed := false
	args := call.Args
	if !call.IsInvoke() && call.Value != nil {
		args = append([]ssa.Value{call.Value}, args...)
	}
	for _, arg := range args {
		if arg == nil {
			continue
		}
		changed = sum.raiseSide(tok[arg], k, false) || changed
		changed = sum.raiseSide(derived[arg], k, true) || changed
		changed = sum.raiseSide(content[arg], k, false) || changed
	}
	return changed
}

func unknownArgs(sum *Summary, call *ssa.CallCommon, tok, derived, content map[ssa.Value]pset) bool {
	changed := false
	for _, arg := range call.Args {
		changed = sum.markUnknown(tok[arg]) || changed
		changed = sum.markUnknown(derived[arg]) || changed
		changed = sum.markUnknown(content[arg]) || changed
	}
	return changed
}

func resultCount(call *ssa.CallCommon) int {
	sig := call.Signature()
	if sig == nil || sig.Results() == nil {
		return 0
	}
	return sig.Results().Len()
}

func (s *Summary) addAlias(ids pset, result int) bool {
	changed := false
	for id := range ids {
		if id < 0 || id >= len(s.Params) {
			continue
		}
		if slices.Contains(s.Params[id].Alias, result) {
			continue
		}
		s.Params[id].Alias = append(s.Params[id].Alias, result)
		slices.Sort(s.Params[id].Alias)
		changed = true
	}
	return changed
}

func (s *Summary) raiseSide(ids pset, k escapeKind, content bool) bool {
	if k == escapeNone || len(ids) == 0 {
		return false
	}
	changed := false
	for id := range ids {
		if id < 0 || id >= len(s.Params) {
			continue
		}
		dst := &s.Params[id].Side
		if content {
			dst = &s.Params[id].Content
		}
		if uint8(k) > *dst {
			*dst = uint8(k)
			changed = true
		}
	}
	return changed
}

func (s *Summary) markUnknown(ids pset) bool {
	changed := false
	for id := range ids {
		if id < 0 || id >= len(s.Params) || s.Params[id].Unknown {
			continue
		}
		s.Params[id].Unknown = true
		changed = true
	}
	return changed
}

func mergedParam(targets []*ssa.Function, index int, lookup func(*ssa.Function) *Summary) (Param, bool) {
	if len(targets) == 0 {
		return Param{}, false
	}
	var acc Param
	set := false
	for _, fn := range targets {
		s := lookup(fn)
		if s == nil || index >= len(s.Params) {
			return Param{Unknown: true}, true
		}
		item := s.Params[index]
		item.Alias = slices.Clone(item.Alias)
		if !set {
			acc = item
			set = true
			continue
		}
		acc = mergeParam(acc, item)
	}
	return acc, set
}

func mergeParam(a, b Param) Param {
	if a.Unknown || b.Unknown {
		return Param{Unknown: true}
	}
	if b.Side > a.Side {
		a.Side = b.Side
	}
	if b.Content > a.Content {
		a.Content = b.Content
	}
	for _, ri := range b.Alias {
		if slices.Contains(a.Alias, ri) {
			continue
		}
		a.Alias = append(a.Alias, ri)
	}
	slices.Sort(a.Alias)
	return a
}

func callTargets(call *ssa.CallCommon, concrete map[ssa.Value][]types.Type, knownFn map[ssa.Value][]*ssa.Function, prog *ssa.Program) ([]*ssa.Function, bool) {
	if _, ok := builtinName(call); ok {
		return nil, true
	}
	if fn := call.StaticCallee(); fn != nil {
		return []*ssa.Function{fn}, true
	}
	if call.IsInvoke() {
		tys, ok := concrete[call.Value]
		if !ok || len(tys) == 0 || call.Method == nil {
			return nil, false
		}
		var out []*ssa.Function
		for _, typ := range tys {
			fn := methodFunc(prog, typ, call.Method)
			if fn == nil {
				return nil, false
			}
			out = append(out, fn)
		}
		return out, len(out) > 0
	}
	if fn, ok := call.Value.(*ssa.Function); ok {
		return []*ssa.Function{fn}, true
	}
	if clo, ok := call.Value.(*ssa.MakeClosure); ok {
		if fn, ok := clo.Fn.(*ssa.Function); ok {
			return []*ssa.Function{fn}, true
		}
	}
	fns, ok := knownFn[call.Value]
	return fns, ok && len(fns) > 0
}

func methodFunc(prog *ssa.Program, typ types.Type, method *types.Func) *ssa.Function {
	if prog == nil || method == nil {
		return nil
	}
	sel := types.NewMethodSet(typ).Lookup(method.Pkg(), method.Name())
	if sel == nil {
		if _, isPtr := typ.Underlying().(*types.Pointer); !isPtr {
			sel = types.NewMethodSet(types.NewPointer(typ)).Lookup(method.Pkg(), method.Name())
		}
	}
	if sel == nil {
		return nil
	}
	return prog.MethodValue(sel)
}

func localFacts(instrs []ssa.Instruction) (map[ssa.Value][]types.Type, map[ssa.Value][]*ssa.Function) {
	concrete := map[ssa.Value][]types.Type{}
	memT := map[ssa.Value][]types.Type{}
	knownFn := map[ssa.Value][]*ssa.Function{}
	memF := map[ssa.Value][]*ssa.Function{}
	users := map[ssa.Value][]ssa.Instruction{}
	var rands []*ssa.Value
	for _, instr := range instrs {
		rands = instr.Operands(rands[:0])
		for _, op := range rands {
			if op == nil || *op == nil {
				continue
			}
			users[*op] = append(users[*op], instr)
		}
	}
	queue := slices.Clone(instrs)
	inq := make(map[ssa.Instruction]bool, len(queue))
	for _, instr := range queue {
		inq[instr] = true
	}
	limit := min(len(instrs)*len(instrs)+8, 100000)
	for head := 0; head < len(queue); head++ {
		if head > limit {
			break
		}
		instr := queue[head]
		inq[instr] = false
		changed, addrs := stepFacts(instr, concrete, memT, knownFn, memF)
		if !changed {
			continue
		}
		wake := addrs
		if value, ok := instr.(ssa.Value); ok {
			wake = append(wake, value)
		}
		for _, v := range wake {
			for _, user := range users[v] {
				if inq[user] {
					continue
				}
				inq[user] = true
				queue = append(queue, user)
			}
		}
	}
	return concrete, knownFn
}

func stepFacts(instr ssa.Instruction, concrete, memT map[ssa.Value][]types.Type, knownFn, memF map[ssa.Value][]*ssa.Function) (bool, []ssa.Value) {
	switch in := instr.(type) {
	case *ssa.MakeInterface:
		return setTypes(concrete, in, []types.Type{in.X.Type()}), nil
	case *ssa.MakeClosure:
		fn, ok := in.Fn.(*ssa.Function)
		if !ok {
			return false, nil
		}
		return setFns(knownFn, in, []*ssa.Function{fn}), nil
	case *ssa.ChangeType:
		changed := false
		if tys, ok := concrete[in.X]; ok {
			changed = setTypes(concrete, in, tys)
		}
		if fn, ok := in.X.(*ssa.Function); ok {
			changed = setFns(knownFn, in, []*ssa.Function{fn}) || changed
		} else if fns, ok := knownFn[in.X]; ok {
			changed = setFns(knownFn, in, fns) || changed
		}
		return changed, nil
	case *ssa.Phi:
		changed := false
		if tys, ok := allTypes(in.Edges, concrete); ok {
			changed = setTypes(concrete, in, tys)
		}
		if fns, ok := allFns(in.Edges, knownFn); ok {
			changed = setFns(knownFn, in, fns) || changed
		}
		return changed, nil
	case *ssa.Store:
		changed := false
		if tys, ok := concrete[in.Val]; ok {
			changed = setTypes(memT, in.Addr, tys)
		}
		if fns, ok := knownFn[in.Val]; ok {
			changed = setFns(memF, in.Addr, fns) || changed
		}
		if !changed {
			return false, nil
		}
		return true, []ssa.Value{in.Addr}
	case *ssa.UnOp:
		if in.Op != token.MUL {
			return false, nil
		}
		changed := false
		if tys, ok := memT[in.X]; ok {
			changed = setTypes(concrete, in, tys)
		}
		if fns, ok := memF[in.X]; ok {
			changed = setFns(knownFn, in, fns) || changed
		}
		return changed, nil
	default:
		return false, nil
	}
}

func allTypes(edges []ssa.Value, known map[ssa.Value][]types.Type) ([]types.Type, bool) {
	if len(edges) == 0 {
		return nil, false
	}
	var out []types.Type
	for _, edge := range edges {
		tys, ok := known[edge]
		if !ok {
			return nil, false
		}
		out = unionTypes(out, tys)
	}
	return out, true
}

func allFns(edges []ssa.Value, known map[ssa.Value][]*ssa.Function) ([]*ssa.Function, bool) {
	if len(edges) == 0 {
		return nil, false
	}
	var out []*ssa.Function
	for _, edge := range edges {
		fns, ok := known[edge]
		if !ok {
			return nil, false
		}
		out = unionFns(out, fns)
	}
	return out, true
}

func setTypes(m map[ssa.Value][]types.Type, v ssa.Value, tys []types.Type) bool {
	if slices.Equal(m[v], tys) && (len(tys) > 0 || len(m[v]) > 0) {
		return false
	}
	if len(tys) == 0 && len(m[v]) == 0 {
		if _, ok := m[v]; ok {
			return false
		}
	}
	m[v] = slices.Clone(tys)
	return true
}

func setFns(m map[ssa.Value][]*ssa.Function, v ssa.Value, fns []*ssa.Function) bool {
	if slices.Equal(m[v], fns) && (len(fns) > 0 || len(m[v]) > 0) {
		if _, ok := m[v]; ok || len(fns) == 0 {
			if len(fns) == 0 {
				if _, ok := m[v]; ok {
					return false
				}
			} else if slices.Equal(m[v], fns) {
				return false
			}
		}
	}
	if slices.Equal(m[v], fns) {
		_, ok := m[v]
		if ok || len(fns) == 0 {
			return false
		}
	}
	m[v] = slices.Clone(fns)
	return true
}

func unionTypes(a, b []types.Type) []types.Type {
	out := slices.Clone(a)
	for _, item := range b {
		if !slices.Contains(out, item) {
			out = append(out, item)
		}
	}
	return out
}

func unionFns(a, b []*ssa.Function) []*ssa.Function {
	out := slices.Clone(a)
	for _, item := range b {
		if !slices.Contains(out, item) {
			out = append(out, item)
		}
	}
	return out
}

func unionAt(m map[ssa.Value]pset, v ssa.Value, src pset) bool {
	if v == nil || len(src) == 0 {
		return false
	}
	dst := m[v]
	if dst == nil {
		dst = pset{}
		m[v] = dst
	}
	changed := false
	for id := range src {
		if _, ok := dst[id]; ok {
			continue
		}
		dst[id] = struct{}{}
		changed = true
	}
	return changed
}

func mergeSets(a, b pset) pset {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	out := pset{}
	for id := range a {
		out[id] = struct{}{}
	}
	for id := range b {
		out[id] = struct{}{}
	}
	return out
}

func operandSets(instr ssa.Instruction, tok, derived map[ssa.Value]pset) (pset, pset) {
	var t, d pset
	for _, rand := range instr.Operands(nil) {
		if rand == nil || *rand == nil {
			continue
		}
		t = mergeSets(t, tok[*rand])
		d = mergeSets(d, derived[*rand])
	}
	return t, d
}

func flowsEqual(a, b []pset) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return false
		}
		for id := range a[i] {
			if _, ok := b[i][id]; !ok {
				return false
			}
		}
	}
	return true
}

func addressBase(v ssa.Value) ssa.Value {
	seen := map[ssa.Value]bool{}
	for v != nil && !seen[v] {
		seen[v] = true
		switch x := v.(type) {
		case *ssa.FieldAddr:
			v = x.X
		case *ssa.IndexAddr:
			v = x.X
		case *ssa.Slice:
			v = x.X
		case *ssa.ChangeType:
			v = x.X
		case *ssa.Convert:
			v = x.X
		default:
			return v
		}
	}
	return v
}

func linkCallAliases(instrs []ssa.Instruction, pts map[ssa.Value]siteSet, v1 *v1Ctx) {
	var concrete map[ssa.Value][]types.Type
	var knownFn map[ssa.Value][]*ssa.Function
	if len(instrs) > 0 {
		got := v1.facts(instrs[0].Parent())
		concrete, knownFn = got.concrete, got.fns
	}
	extracts := map[*ssa.Call]map[int]*ssa.Extract{}
	for _, instr := range instrs {
		ex, ok := instr.(*ssa.Extract)
		if !ok {
			continue
		}
		call, ok := ex.Tuple.(*ssa.Call)
		if !ok {
			continue
		}
		if extracts[call] == nil {
			extracts[call] = map[int]*ssa.Extract{}
		}
		extracts[call][ex.Index] = ex
	}
	changed := true
	for changed {
		changed = false
		for _, instr := range instrs {
			call, ok := instr.(ssa.CallInstruction)
			if !ok {
				continue
			}
			if _, isGo := instr.(*ssa.Go); isGo {
				continue
			}
			common := call.Common()
			if _, builtin := builtinName(common); builtin {
				continue
			}
			targets, known := callTargets(common, concrete, knownFn, callerProg(instr))
			if !known {
				continue
			}
			nres := resultCount(common)
			for i, arg := range common.Args {
				ps, ok := mergedParam(targets, summaryIndex(common, i), func(fn *ssa.Function) *Summary {
					return summaryOf(v1, fn)
				})
				if !ok || ps.Unknown {
					continue
				}
				for _, ri := range ps.Alias {
					dst := resultValue(instr, ri, nres, extracts)
					if dst == nil {
						continue
					}
					if absorbSites(pts, dst, pts[arg]) {
						changed = true
					}
				}
			}
		}
		for _, instr := range instrs {
			if flow(instr, func(v ssa.Value, src siteSet) bool { return absorbSites(pts, v, src) }, pts) {
				changed = true
			}
			if memory(instr, pts, v1Contents(v1), func(v ssa.Value, src siteSet) bool { return absorbSites(pts, v, src) }, func(obj ssa.Instruction, src siteSet) bool {
				return holdSites(v1, obj, src)
			}) {
				changed = true
			}
		}
	}
}

func v1Contents(v1 *v1Ctx) map[ssa.Instruction]siteSet {
	if v1 == nil {
		return nil
	}
	return v1.contents
}

func holdSites(v1 *v1Ctx, obj ssa.Instruction, src siteSet) bool {
	if v1 == nil || obj == nil || len(src) == 0 {
		return false
	}
	if v1.contents == nil {
		v1.contents = map[ssa.Instruction]siteSet{}
	}
	dst := v1.contents[obj]
	if dst == nil {
		dst = siteSet{}
		v1.contents[obj] = dst
	}
	return dst.absorb(src)
}

func callerProg(instr ssa.Instruction) *ssa.Program {
	call, ok := instr.(ssa.CallInstruction)
	if !ok {
		return nil
	}
	if fn := call.Common().StaticCallee(); fn != nil {
		return fn.Prog
	}
	if parent := instr.Parent(); parent != nil {
		return parent.Prog
	}
	return nil
}

func (v1 *v1Ctx) facts(fn *ssa.Function) resolved {
	if v1 == nil {
		return resolved{}
	}
	if v1.seen == nil {
		v1.seen = map[*ssa.Function]resolved{}
	}
	return remember(v1.seen, fn)
}

func summaryOf(v1 *v1Ctx, fn *ssa.Function) *Summary {
	if v1 == nil || fn == nil {
		return unknownSummary(fn)
	}
	if s, ok := v1.sums[fn]; ok {
		return s
	}
	if fn.Object() != nil {
		for item, s := range v1.sums {
			if item != nil && item.Object() == fn.Object() {
				return s
			}
		}
	}
	return importSummary(v1.pass, fn)
}

func resultValue(instr ssa.Instruction, index, nres int, extracts map[*ssa.Call]map[int]*ssa.Extract) ssa.Value {
	call, ok := instr.(*ssa.Call)
	if !ok || index < 0 || index >= nres {
		return nil
	}
	if nres == 1 {
		return call
	}
	return extracts[call][index]
}

func absorbSites(pts map[ssa.Value]siteSet, v ssa.Value, src siteSet) bool {
	if v == nil || len(src) == 0 {
		return false
	}
	dst := pts[v]
	if dst == nil {
		dst = siteSet{}
		pts[v] = dst
	}
	return dst.absorb(src)
}

func (v1 *v1Ctx) transfer(instr ssa.Instruction, call *ssa.CallCommon, pts map[ssa.Value]siteSet, escape map[ssa.Instruction]escapeKind, notes map[ssa.Instruction]transferNote) {
	fn := instr.Parent()
	var prog *ssa.Program
	if fn != nil {
		prog = fn.Prog
	}
	got := v1.facts(fn)
	targets, known := callTargets(call, got.concrete, got.fns, prog)
	for i, arg := range call.Args {
		ps, ok := mergedParam(targets, summaryIndex(call, i), func(item *ssa.Function) *Summary { return summaryOf(v1, item) })
		if !known || !ok || ps.Unknown {
			raise(escape, notes, pts[arg], escapeCall, "shared")
			raiseHeld(v1, pts[arg], escape, notes, escapeCall, "shared")
			continue
		}
		if ps.Side != 0 {
			word := transferWord(instr, pts[arg], pts)
			raise(escape, notes, pts[arg], escapeKind(ps.Side), word)
		}
		if ps.Content != 0 {
			word := transferWord(instr, pts[arg], pts)
			raiseHeld(v1, pts[arg], escape, notes, escapeKind(ps.Content), word)
		}
	}
}

func raiseHeld(v1 *v1Ctx, objs siteSet, escape map[ssa.Instruction]escapeKind, notes map[ssa.Instruction]transferNote, k escapeKind, word string) {
	if v1 == nil {
		return
	}
	for obj := range objs {
		raise(escape, notes, v1.contents[obj], k, word)
	}
}

func transferWord(call ssa.Instruction, sites siteSet, pts map[ssa.Value]siteSet) string {
	if usedAfter(call, sites, pts) {
		return "shared"
	}
	return "move"
}

func usedAfter(call ssa.Instruction, sites siteSet, pts map[ssa.Value]siteSet) bool {
	if len(sites) == 0 {
		return false
	}
	after := instrsAfter(call)
	for value, set := range pts {
		if !intersects(set, sites) {
			continue
		}
		refs := value.Referrers()
		if refs == nil {
			continue
		}
		for _, ref := range *refs {
			if ref == call {
				continue
			}
			if _, ok := ref.(*ssa.DebugRef); ok {
				continue
			}
			if after[ref] {
				return true
			}
		}
	}
	return false
}

func instrsAfter(call ssa.Instruction) map[ssa.Instruction]bool {
	out := map[ssa.Instruction]bool{}
	block := call.Block()
	if block == nil {
		return out
	}
	seen := false
	for _, instr := range block.Instrs {
		if seen {
			out[instr] = true
		}
		if instr == call {
			seen = true
		}
	}
	visited := map[*ssa.BasicBlock]bool{block: true}
	queue := append([]*ssa.BasicBlock{}, block.Succs...)
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		if next == nil || visited[next] {
			continue
		}
		visited[next] = true
		for _, instr := range next.Instrs {
			out[instr] = true
		}
		queue = append(queue, next.Succs...)
	}
	return out
}

func markAppendAlias(instrs []ssa.Instruction, pts map[ssa.Value]siteSet, escape map[ssa.Instruction]escapeKind, notes map[ssa.Instruction]transferNote) {
	for _, instr := range instrs {
		call, ok := instr.(*ssa.Call)
		if !ok {
			continue
		}
		name, ok := builtinName(&call.Call)
		if !ok || name != "append" || len(call.Call.Args) == 0 {
			continue
		}
		if !liveAliasAfter(call, pts) {
			continue
		}
		for site := range pts[call.Call.Args[0]] {
			if escape[site] < escapeCall {
				raise(escape, notes, siteSet{site: {}}, escapeCall, "shared")
			}
		}
	}
}

func liveAliasAfter(call *ssa.Call, pts map[ssa.Value]siteSet) bool {
	arg := call.Call.Args[0]
	sites := pts[arg]
	if len(sites) == 0 {
		return false
	}
	after := instrsAfter(call)
	for value, set := range pts {
		if !intersects(set, sites) || !definedBefore(value, call, after) {
			continue
		}
		refs := value.Referrers()
		if refs == nil {
			continue
		}
		for _, ref := range *refs {
			if ref == call {
				continue
			}
			if _, ok := ref.(*ssa.DebugRef); ok {
				continue
			}
			if after[ref] {
				return true
			}
		}
	}
	return false
}

func definedBefore(value ssa.Value, call ssa.Instruction, after map[ssa.Instruction]bool) bool {
	instr, ok := value.(ssa.Instruction)
	if !ok {
		return true
	}
	if instr == call {
		return false
	}
	return !after[instr]
}

func intersects(a, b siteSet) bool {
	if len(a) > len(b) {
		a, b = b, a
	}
	for site := range a {
		if _, ok := b[site]; ok {
			return true
		}
	}
	return false
}
