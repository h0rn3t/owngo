package cl

import (
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"github.com/xgo-dev/llgo/owngo/census"
	llssa "github.com/xgo-dev/llgo/ssa"
	"golang.org/x/tools/go/ssa"
)

// Номери збігаються з runtime.CatStatic … CatImplicit.
const (
	ownGoStatic   uint64 = 0
	ownGoRCLocal  uint64 = 1
	ownGoRCShared uint64 = 2
	ownGoCyclic   uint64 = 3
	ownGoPinned   uint64 = 4
	ownGoImplicit uint64 = 5
)

func (p *context) prepareOwnGoFree(pkg *ssa.Package) {
	if !p.options.OwnGoFree || pkg == nil {
		return
	}
	funcs := ssaPackageFuncs(pkg)
	cats, uses, decUses, counted, inc := census.Place(funcs)
	p.ownGoUses = uses
	p.ownGoDecUses = decUses
	p.ownGoCounted = counted
	p.ownGoInc = inc
	if len(cats) == 0 {
		return
	}
	p.ownGoCat = make(map[ssa.Instruction]uint64, len(cats))
	for site, label := range cats {
		cat, ok := ownGoCategory(label)
		if !ok {
			continue
		}
		p.ownGoCat[site] = cat
	}
}

func ownGoCategory(label string) (uint64, bool) {
	head, _, _ := strings.Cut(label, ":")
	switch head {
	case "static":
		return ownGoStatic, true
	case "rc-local":
		return ownGoRCLocal, true
	case "rc-shared":
		return ownGoRCShared, true
	case "cyclic":
		return ownGoCyclic, true
	case "pinned":
		return ownGoPinned, true
	case "implicit":
		return ownGoImplicit, true
	default:
		return 0, false
	}
}

func ssaPackageFuncs(pkg *ssa.Package) []*ssa.Function {
	if pkg == nil || pkg.Prog == nil || pkg.Pkg == nil {
		return nil
	}
	seen := map[*ssa.Function]bool{}
	var out []*ssa.Function
	var add func(*ssa.Function)
	add = func(fn *ssa.Function) {
		if fn == nil || seen[fn] {
			return
		}
		seen[fn] = true
		out = append(out, fn)
		for _, anon := range fn.AnonFuncs {
			add(anon)
		}
	}
	for _, mem := range pkg.Members {
		if fn, ok := mem.(*ssa.Function); ok {
			add(fn)
		}
	}
	for _, name := range pkg.Pkg.Scope().Names() {
		obj, ok := pkg.Pkg.Scope().Lookup(name).(*types.TypeName)
		if !ok {
			continue
		}
		named, ok := obj.Type().(*types.Named)
		if !ok || named.TypeParams() != nil {
			continue
		}
		for _, typ := range []types.Type{named, types.NewPointer(named)} {
			mset := pkg.Prog.MethodSets.MethodSet(typ)
			for sel := range mset.Methods() {
				add(pkg.Prog.FuncValue(sel.Obj().(*types.Func)))
			}
		}
	}
	return out
}

// ownGoFreeAfter ставить free на останньому читанні в блоці, після якого
// об'єкт уже не живий. Гілка, де читання лишається попереду, free не отримує:
// об'єкт доживає до виходу процесу. defer тримає аргумент до RunDefers, тож
// таке місце в цій фазі не звільняється.
func ownGoFreeAfter(fn *ssa.Function, uses map[ssa.Instruction][]ssa.Instruction, keepStored bool) map[ssa.Instruction][]ssa.Value {
	if fn == nil || len(fn.Blocks) == 0 || len(uses) == 0 {
		return nil
	}
	out := map[ssa.Instruction][]ssa.Value{}
	for site, users := range uses {
		if site == nil || site.Parent() != fn || deferred(users) {
			continue
		}
		val, ok := site.(ssa.Value)
		if !ok {
			continue
		}
		// Вказівник, записаний у пам'ять, читають уже не з цього значення SSA.
		// Free за останнім прямим читанням звільнив би об'єкт перед тим записом.
		if !keepStored && pointerStored(val, users) {
			continue
		}
		user := map[ssa.Instruction]bool{}
		for _, instr := range users {
			if instr != nil && instr.Parent() == fn {
				user[instr] = true
			}
		}
		for instr := range deaths(fn, user, site) {
			// Використання аліаса в блоці, якого алокація не домінує,
			// не має місця для free: значення LLVM там недоступне.
			if instr.Block() == nil || site.Block() == nil || !site.Block().Dominates(instr.Block()) {
				continue
			}
			out[instr] = append(out[instr], val)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// pointerStored звітує, що саме значення вказівника кладуть у пам'ять,
// а не дані за цим вказівником.
func pointerStored(site ssa.Value, users []ssa.Instruction) bool {
	seen := map[ssa.Value]bool{}
	for _, user := range users {
		switch user := user.(type) {
		case *ssa.Store:
			if carriesPointer(user.Val, site, seen) {
				return true
			}
		case *ssa.MapUpdate:
			if carriesPointer(user.Key, site, seen) || carriesPointer(user.Value, site, seen) {
				return true
			}
		}
	}
	return false
}

func carriesPointer(v, site ssa.Value, seen map[ssa.Value]bool) bool {
	if v == nil || seen[v] {
		return false
	}
	seen[v] = true
	if v == site {
		return true
	}
	switch v := v.(type) {
	case *ssa.Phi:
		for _, edge := range v.Edges {
			if carriesPointer(edge, site, seen) {
				return true
			}
		}
	case *ssa.ChangeType:
		return carriesPointer(v.X, site, seen)
	case *ssa.Convert:
		return carriesPointer(v.X, site, seen)
	case *ssa.Extract:
		return carriesPointer(v.Tuple, site, seen)
	}
	return false
}
func deferred(users []ssa.Instruction) bool {
	for _, user := range users {
		if _, ok := user.(*ssa.Defer); ok {
			return true
		}
	}
	return false
}

// deaths шукає блоки, після яких місце вже не живе. Блок визначення
// живучість назад не пропускає, а читач у ньому після site живим на вході
// його не робить: кожне виконання site створює новий об'єкт, тож через
// зворотне ребро циклу попередній об'єкт живим не лишається. Об'єкт, який
// несе phi, має свого читача в заголовку і лишається живим.
func deaths(fn *ssa.Function, user map[ssa.Instruction]bool, site ssa.Instruction) map[ssa.Instruction]struct{} {
	n := len(fn.Blocks)
	def := site.Block()
	used := make([]bool, n)
	for instr := range user {
		block := instr.Block()
		if block == nil || block.Parent() != fn || block.Index < 0 || block.Index >= n {
			continue
		}
		if block == def && !precedes(instr, site) {
			continue
		}
		used[block.Index] = true
	}
	liveIn := make([]bool, n)
	liveOut := make([]bool, n)
	for {
		changed := false
		for i := n - 1; i >= 0; i-- {
			out := false
			for _, succ := range fn.Blocks[i].Succs {
				if succ != nil && succ.Index >= 0 && succ.Index < n && liveIn[succ.Index] {
					out = true
					break
				}
			}
			in := used[i] || out && fn.Blocks[i] != def
			if in != liveIn[i] || out != liveOut[i] {
				liveIn[i], liveOut[i] = in, out
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	out := map[ssa.Instruction]struct{}{}
	for _, block := range fn.Blocks {
		if block == nil || !liveIn[block.Index] && block != def || liveOut[block.Index] {
			continue
		}
		var last ssa.Instruction
		for _, instr := range block.Instrs {
			if user[instr] {
				last = instr
			}
		}
		if last != nil {
			out[last] = struct{}{}
		}
	}
	return out
}

func (p *context) emitOwnGoNote(b llssa.Builder, instr ssa.Instruction) {
	cat, ok := p.ownGoCat[instr]
	if !ok {
		return
	}
	val, ok := instr.(ssa.Value)
	if !ok {
		return
	}
	if _, has := p.bvals[val]; !has {
		return
	}
	if _, ok := p.ownGoSiteSize(val); !ok || ownGoFreshCall(val) {
		// Свіжий результат уже облікував викликаний код.
		return
	}
	p.callOwnGoAccount(b, "NoteAlloc", llssa.Nil, p.ownGoSizeExpr(b, val), cat)
}

func (p *context) emitOwnGoFree(b llssa.Builder, instr ssa.Instruction) {
	defer p.emitOwnGoDec(b, instr)
	sites := p.ownGoFreeAfter[instr]
	if len(sites) == 0 {
		return
	}
	for _, site := range sites {
		ptr := p.ownGoFreePtr(b, site)
		if ptr.IsNil() {
			continue
		}
		_, sized := p.ownGoSiteSize(site)
		cat, known := ownGoStatic, false
		if siteInstr, isInstr := site.(ssa.Instruction); isInstr {
			cat, known = p.ownGoCat[siteInstr]
		}
		size := p.prog.IntVal(0, p.prog.Uintptr())
		if sized && known {
			size = p.ownGoSizeExpr(b, site)
		} else {
			cat = ownGoStatic
		}
		p.callOwnGoAccount(b, "FreeAccounted", ptr, size, cat)
	}
}

func (p *context) emitOwnGoDec(b llssa.Builder, instr ssa.Instruction) {
	sites := p.ownGoDecAfter[instr]
	if len(sites) == 0 {
		return
	}
	for _, site := range sites {
		ptr := p.ownGoFreePtr(b, site)
		if ptr.IsNil() {
			continue
		}
		if _, sized := p.ownGoSiteSize(site); !sized {
			continue
		}
		if instr, ok := site.(ssa.Instruction); ok && p.ownGoCat[instr] == ownGoCyclic {
			p.callDecCyclic(b, ptr, site.Type().Underlying().(*types.Pointer).Elem())
			continue
		}
		p.callOwnGoAccount(b, "DecAccounted", ptr, p.ownGoSizeExpr(b, site), 0)
	}
}

// emitOwnGoStoreInc рахує вказівник місця cyclic, записаний у поле обходу.
func (p *context) emitOwnGoStoreInc(b llssa.Builder, store *ssa.Store, val llssa.Expr) {
	if len(p.ownGoInc[store]) == 0 {
		return
	}
	p.callOwnGoAccount(b, "Inc", val, llssa.Nil, 0)
}

func (p *context) callDecCyclic(b llssa.Builder, ptr llssa.Expr, elem types.Type) {
	unsafePtr := types.Typ[types.UnsafePointer]
	sig := types.NewSignatureType(nil, nil, nil, types.NewTuple(
		types.NewVar(token.NoPos, nil, "ptr", unsafePtr),
		types.NewVar(token.NoPos, nil, "desc", unsafePtr),
	), nil, false)
	dst := p.type_(unsafePtr, llssa.GoDecl)
	b.Call(p.runtimeFunc("DecCyclic", sig), b.PtrCast(dst, ptr), b.PtrCast(dst, p.cyclicDesc(elem)))
}

// cyclicDesc повертає глобальну runtime.CyclicDesc для elem: розмір і поля
// з census.CyclicFields з дескрипторами їхніх цілей. Розкладка збігається
// з runtime.CyclicDesc і runtime.CyclicEdge.
func (p *context) cyclicDesc(elem types.Type) llssa.Expr {
	if desc, ok := p.ownGoDescs[elem]; ok {
		return desc
	}
	if p.ownGoDescs == nil {
		p.ownGoDescs = map[types.Type]llssa.Expr{}
	}
	uintptrT, unsafePtr := types.Typ[types.Uintptr], types.Typ[types.UnsafePointer]
	edgeT := types.NewStruct([]*types.Var{
		types.NewField(token.NoPos, nil, "Off", uintptrT, false),
		types.NewField(token.NoPos, nil, "Elem", unsafePtr, false),
	}, nil)
	edgesT := types.NewSlice(edgeT)
	descT := types.NewStruct([]*types.Var{
		types.NewField(token.NoPos, nil, "Size", uintptrT, false),
		types.NewField(token.NoPos, nil, "Edges", edgesT, false),
	}, nil)
	name := p.pkg.Path() + ".__owngo_cyclic$" + strconv.Itoa(len(p.ownGoDescs))
	global := p.pkg.NewVarEx(name, p.prog.Pointer(p.type_(descT, llssa.GoDecl)))
	// Запис до обходу полів: дескриптор може вказувати сам на себе.
	p.ownGoDescs[elem] = global.Expr

	elemT := p.type_(elem, llssa.GoDecl)
	fields := census.CyclicFields(elem)
	edges := p.prog.Zero(p.type_(edgesT, llssa.GoDecl))
	if len(fields) > 0 {
		values := make([]llssa.Expr, len(fields))
		st := elem.Underlying().(*types.Struct)
		for i, field := range fields {
			target := st.Field(field).Type().Underlying().(*types.Pointer).Elem()
			values[i] = p.prog.ConstStruct(p.type_(edgeT, llssa.GoDecl), []llssa.Expr{
				p.prog.IntVal(p.prog.OffsetOf(elemT, field), p.prog.Uintptr()),
				p.cyclicDesc(target),
			})
		}
		edges = p.pkg.ConstSlice(name+"$edges", p.type_(edgesT, llssa.GoDecl), values)
	}
	global.Init(p.prog.ConstStruct(p.type_(descT, llssa.GoDecl), []llssa.Expr{
		p.prog.IntVal(p.prog.SizeOf(elemT), p.prog.Uintptr()),
		edges,
	}))
	return global.Expr
}

func (p *context) emitOwnGoInc(b llssa.Builder, instr ssa.Instruction) {
	sites := p.ownGoInc[instr]
	if len(sites) == 0 {
		return
	}
	for _, site := range sites {
		if _, ok := p.bvals[site]; !ok {
			p.compileValue(b, site)
		}
		ptr := p.ownGoFreePtr(b, site)
		if ptr.IsNil() {
			continue
		}
		if _, sized := p.ownGoSiteSize(site); !sized {
			continue
		}
		p.callOwnGoAccount(b, "Inc", ptr, llssa.Nil, 0)
	}
}

func (p *context) emitAllocCounted(b llssa.Builder, elem llssa.Type) llssa.Expr {
	sig := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(types.NewVar(token.NoPos, nil, "size", types.Typ[types.Uintptr])),
		types.NewTuple(types.NewVar(token.NoPos, nil, "ptr", types.Typ[types.UnsafePointer])),
		false)
	raw := b.Call(p.runtimeFunc("AllocCounted", sig), p.prog.IntVal(p.prog.SizeOf(elem), p.prog.Uintptr()))
	ret := b.PtrCast(p.prog.Pointer(elem), raw)
	b.Store(ret, p.prog.Zero(elem))
	return ret
}

func (p *context) ownGoSiteSize(site ssa.Value) (uint64, bool) {
	if instr, ok := site.(ssa.Instruction); ok && census.CountableImplicit(instr) {
		// Виклик лишається. Байти рахує ownGoSizeExpr, бо довжина відома лише в IR.
		return 0, true
	}
	alloc, ok := site.(*ssa.Alloc)
	if (!ok || !alloc.Heap) && !ownGoFreshCall(site) {
		return 0, false
	}
	ptr, ok := types.Unalias(site.Type()).Underlying().(*types.Pointer)
	if !ok {
		return 0, false
	}
	n := p.prog.SizeOf(p.type_(ptr.Elem(), llssa.GoDecl))
	return n, n > 0
}

func (p *context) callOwnGoAccount(b llssa.Builder, name string, ptr llssa.Expr, size llssa.Expr, cat uint64) {
	params := []*types.Var{
		types.NewVar(token.NoPos, nil, "size", types.Typ[types.Uintptr]),
		types.NewVar(token.NoPos, nil, "cat", types.Typ[types.Int32]),
	}
	args := []llssa.Expr{
		size,
		p.prog.IntVal(cat, p.prog.Int32()),
	}
	// Keep each runtimeFunc name a string literal: the runtime audit
	// rejects dynamic compiler lookups.
	switch name {
	case "FreeAccounted":
		params = append([]*types.Var{
			types.NewVar(token.NoPos, nil, "ptr", types.Typ[types.UnsafePointer]),
		}, params...)
		dst := p.type_(types.Typ[types.UnsafePointer], llssa.GoDecl)
		args = append([]llssa.Expr{b.PtrCast(dst, ptr)}, args...)
		sig := types.NewSignatureType(nil, nil, nil, types.NewTuple(params...), nil, false)
		b.Call(p.runtimeFunc("FreeAccounted", sig), args...)
		return
	case "DecAccounted":
		params = []*types.Var{
			types.NewVar(token.NoPos, nil, "ptr", types.Typ[types.UnsafePointer]),
			types.NewVar(token.NoPos, nil, "size", types.Typ[types.Uintptr]),
		}
		dst := p.type_(types.Typ[types.UnsafePointer], llssa.GoDecl)
		args = []llssa.Expr{b.PtrCast(dst, ptr), size}
		sig := types.NewSignatureType(nil, nil, nil, types.NewTuple(params...), nil, false)
		b.Call(p.runtimeFunc("DecAccounted", sig), args...)
		return
	case "Inc":
		params = []*types.Var{
			types.NewVar(token.NoPos, nil, "ptr", types.Typ[types.UnsafePointer]),
		}
		dst := p.type_(types.Typ[types.UnsafePointer], llssa.GoDecl)
		args = []llssa.Expr{b.PtrCast(dst, ptr)}
		sig := types.NewSignatureType(nil, nil, nil, types.NewTuple(params...), nil, false)
		b.Call(p.runtimeFunc("Inc", sig), args...)
		return
	default:
		sig := types.NewSignatureType(nil, nil, nil, types.NewTuple(params...), nil, false)
		b.Call(p.runtimeFunc("NoteAlloc", sig), args...)
		return
	}
}

// ownGoSizeExpr повертає байти користувача. append через GrowSlice лишає 0.
func (p *context) ownGoSizeExpr(b llssa.Builder, site ssa.Value) llssa.Expr {
	zero := p.prog.IntVal(0, p.prog.Uintptr())
	instr, ok := site.(ssa.Instruction)
	if !ok || !census.CountableImplicit(instr) {
		n, sized := p.ownGoSiteSize(site)
		if !sized || n == 0 {
			return zero
		}
		return p.prog.IntVal(n, p.prog.Uintptr())
	}
	expr, ok := p.bvals[site]
	if !ok || expr.IsNil() {
		return zero
	}
	if census.ImplicitOrigin(instr) == "append" {
		call, _ := instr.(*ssa.Call)
		if _, counted := p.ownGoCounted[instr]; !counted && !p.ownGoWillFree(call) {
			return zero
		}
	}
	switch underlying := site.Type().Underlying().(type) {
	case *types.Slice:
		elem := p.prog.SizeOf(p.type_(underlying.Elem(), llssa.GoDecl))
		return b.Convert(p.prog.Uintptr(), p.ownGoBytes(b, b.SliceLen(expr), elem))
	case *types.Basic:
		if underlying.Kind() == types.String {
			return b.Convert(p.prog.Uintptr(), b.StringLen(expr))
		}
	}
	return zero
}

func (p *context) ownGoFreePtr(b llssa.Builder, site ssa.Value) llssa.Expr {
	expr, ok := p.bvals[site]
	if !ok || expr.IsNil() {
		return llssa.Nil
	}
	if ownGoFreshCall(site) {
		return expr
	}
	switch site.(type) {
	case *ssa.MakeSlice:
		return b.SliceData(expr)
	case *ssa.MakeInterface:
		return b.InterfaceData(expr)
	case *ssa.MakeClosure:
		return b.Field(expr, 1)
	case *ssa.Alloc, *ssa.MakeMap, *ssa.MakeChan:
		return expr
	default:
		instr, ok := site.(ssa.Instruction)
		if !ok || census.ImplicitOrigin(instr) == "" {
			return llssa.Nil
		}
		switch underlying := site.Type().Underlying().(type) {
		case *types.Slice:
			return b.SliceData(expr)
		case *types.Basic:
			if underlying.Kind() == types.String {
				return b.StringData(expr)
			}
		}
		return llssa.Nil
	}
}

func (p *context) ownGoWillFree(v ssa.Value) bool {
	for _, sites := range p.ownGoFreeAfter {
		for _, site := range sites {
			if site == v {
				return true
			}
		}
	}
	return false
}

func (p *context) ownGoAppendAlloc(call *ssa.Call) (string, bool) {
	if call == nil || len(call.Call.Args) != 2 {
		return "", false
	}
	builtin, ok := call.Call.Value.(*ssa.Builtin)
	if !ok || builtin.Name() != "append" {
		return "", false
	}
	if _, ok := p.ownGoCounted[call]; ok {
		return "AllocCounted", true
	}
	if p.ownGoWillFree(call) {
		return "AllocU", true
	}
	return "", false
}

func (p *context) ownGoCountedString(v *ssa.BinOp) bool {
	if v == nil || v.Op != token.ADD || !isGoString(v.X.Type()) {
		return false
	}
	_, ok := p.ownGoCounted[v]
	return ok
}

func (p *context) ownGoCountedConvert(v *ssa.Convert) bool {
	if v == nil || !byteStringConvertCL(v.X.Type(), v.Type()) {
		return false
	}
	_, ok := p.ownGoCounted[v]
	return ok
}

func (p *context) callSizedAlloc(b llssa.Builder, name string, size llssa.Expr) llssa.Expr {
	sig := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(types.NewVar(token.NoPos, nil, "size", types.Typ[types.Uintptr])),
		types.NewTuple(types.NewVar(token.NoPos, nil, "ptr", types.Typ[types.UnsafePointer])),
		false)
	// Keep each runtimeFunc name a string literal: the runtime audit
	// rejects dynamic compiler lookups.
	if name == "AllocU" {
		return b.Call(p.runtimeFunc("AllocU", sig), b.Convert(p.prog.Uintptr(), size))
	}
	return b.Call(p.runtimeFunc("AllocCounted", sig), b.Convert(p.prog.Uintptr(), size))
}

func (p *context) ownGoBytes(b llssa.Builder, n llssa.Expr, elemSize uint64) llssa.Expr {
	if elemSize == 0 {
		return p.prog.IntVal(0, p.prog.Int())
	}
	if elemSize == 1 {
		return n
	}
	return b.BinOp(token.MUL, n, p.prog.IntVal(elemSize, p.prog.Int()))
}

// emitOwnedAppend виділяє новий буфер. SliceAppend повертає той самий
// data, коли cap вистачає, і FreeAccounted того вказівника звільнив би масив викликача.
func (p *context) emitOwnedAppend(b llssa.Builder, call *ssa.Call, allocName string) llssa.Expr {
	src := p.compileValue(b, call.Call.Args[0])
	sliceTy := call.Type().Underlying().(*types.Slice)
	elemGo := sliceTy.Elem()
	elemLL := p.type_(elemGo, llssa.GoDecl)
	elemSize := p.prog.SizeOf(elemLL)
	oldLen := b.SliceLen(src)
	arg := call.Call.Args[1]
	argExpr := p.compileValue(b, arg)
	var extraPtr llssa.Expr
	var extraCount llssa.Expr
	switch {
	case isGoString(arg.Type()) && isBasicKind(elemGo, types.Byte):
		extraPtr = b.StringData(argExpr)
		extraCount = b.StringLen(argExpr)
	case types.Identical(arg.Type().Underlying(), sliceTy):
		extraPtr = b.SliceData(argExpr)
		extraCount = b.SliceLen(argExpr)
	default:
		slot := b.AllocaT(elemLL)
		b.Store(slot, argExpr)
		extraPtr = slot
		extraCount = p.prog.IntVal(1, p.prog.Int())
	}
	newLen := b.BinOp(token.ADD, oldLen, extraCount)
	raw := p.callSizedAlloc(b, allocName, p.ownGoBytes(b, newLen, elemSize))
	elemPtr := b.PtrCast(p.prog.Pointer(elemLL), raw)
	b.CopyBytes(elemPtr, b.SliceData(src), p.ownGoBytes(b, oldLen, elemSize))
	b.CopyBytes(b.Advance(elemPtr, oldLen), extraPtr, p.ownGoBytes(b, extraCount, elemSize))
	return b.SliceFromData(elemPtr, newLen, newLen)
}

func (p *context) emitCountedStringCat(b llssa.Builder, v *ssa.BinOp) llssa.Expr {
	x := p.compileValue(b, v.X)
	y := p.compileValue(b, v.Y)
	nx := b.StringLen(x)
	ny := b.StringLen(y)
	n := b.BinOp(token.ADD, nx, ny)
	raw := p.callSizedAlloc(b, "AllocCounted", n)
	dst := b.PtrCast(p.prog.Pointer(p.prog.Byte()), raw)
	b.CopyBytes(dst, b.StringData(x), nx)
	b.CopyBytes(b.Advance(dst, nx), b.StringData(y), ny)
	return b.StringFromData(dst, n)
}

func (p *context) emitCountedConvert(b llssa.Builder, v *ssa.Convert) llssa.Expr {
	x := p.compileValue(b, v.X)
	if isGoString(v.Type()) {
		n := b.SliceLen(x)
		raw := p.callSizedAlloc(b, "AllocCounted", n)
		dst := b.PtrCast(p.prog.Pointer(p.prog.Byte()), raw)
		b.CopyBytes(dst, b.SliceData(x), n)
		return b.StringFromData(dst, n)
	}
	n := b.StringLen(x)
	raw := p.callSizedAlloc(b, "AllocCounted", n)
	dst := b.PtrCast(p.prog.Pointer(p.prog.Byte()), raw)
	b.CopyBytes(dst, b.StringData(x), n)
	return b.SliceFromData(dst, n, n)
}

func isGoString(t types.Type) bool {
	return isBasicKind(t, types.String)
}

func isBasicKind(t types.Type, kind types.BasicKind) bool {
	basic, ok := t.Underlying().(*types.Basic)
	return ok && basic.Kind() == kind
}

func byteStringConvertCL(from, to types.Type) bool {
	return isGoString(from) && isByteSlice(to) || isGoString(to) && isByteSlice(from)
}

func isByteSlice(t types.Type) bool {
	slice, ok := t.Underlying().(*types.Slice)
	if !ok {
		return false
	}
	return isBasicKind(slice.Elem(), types.Byte)
}

// ownGoFreshCall: місце — свіжий виклик census (не append). Слово дав
// викликаний код, викликач його лише зменшує.
func ownGoFreshCall(site ssa.Value) bool {
	call, ok := site.(*ssa.Call)
	return ok && census.ImplicitOrigin(call) == ""
}

// precedes: a стоїть у тому самому блоці раніше за b.
func precedes(a, b ssa.Instruction) bool {
	for _, instr := range a.Block().Instrs {
		switch instr {
		case a:
			return true
		case b:
			return false
		}
	}
	return false
}
