package cl

import (
	"cmp"
	"fmt"
	"go/token"
	"go/types"
	"slices"
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
	p.ownGoStackPuns(funcs)
	place := census.PlaceAll(funcs)
	if p.options.OwnGoStack {
		p.ownGoStackStatic(place)
		ownGoConstIfaces(place)
	}
	cats := place.Cats
	p.ownGoUses = place.StaticUses
	p.ownGoDecUses = place.DecUses
	p.ownGoCounted = place.Counted
	p.ownGoInc = place.Inc
	p.ownGoShared = place.Shared
	p.ownGoGoShared = place.GoShared
	p.ownGoRegions = place.Regions
	p.ownGoBuilders = place.Builders
	p.ownGoLabel = cats
	if len(cats) == 0 {
		return
	}
	p.ownGoCat = make(map[ssa.Instruction]uint64, len(cats))
	var pinned []token.Position
	for site, label := range cats {
		cat, ok := ownGoCategory(label)
		if !ok {
			continue
		}
		p.ownGoCat[site] = cat
		if cat == ownGoPinned && p.options.OwnGoWarn != nil {
			pinned = append(pinned, pkg.Prog.Fset.Position(site.Pos()))
		}
	}
	slices.SortFunc(pinned, func(a, b token.Position) int {
		return cmp.Or(strings.Compare(a.Filename, b.Filename), cmp.Compare(a.Line, b.Line), cmp.Compare(a.Column, b.Column))
	})
	for _, pos := range pinned {
		// Попередження не зупиняє збірку: помилці запису нікуди йти.
		_, _ = fmt.Fprintf(p.options.OwnGoWarn, "%v: owngo: pinned: allocation converted to unsafe.Pointer stays live until exit\n", pos)
	}
}

// ownGoStackPuns знімає Heap зі змінної, чию адресу лише перетворюють в інший
// вказівник і через нього читають чи пишуть у межах функції:
// *(*uint64)(unsafe.Pointer(&f)). go/ssa ставить Heap кожній змінній з узятою
// адресою, census бачить unsafe.Pointer і дає pinned; у gc така змінна на
// стеку. Без Heap census дає їй stack, а кодоген — alloca.
func (p *context) ownGoStackPuns(funcs []*ssa.Function) {
	for _, fn := range funcs {
		if fn.TypeParams().Len() > len(fn.TypeArgs()) {
			continue // тіло узагальненої функції: розміри типів невідомі
		}
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				if alloc, ok := instr.(*ssa.Alloc); ok && alloc.Heap && p.ownGoLocalPun(alloc) {
					alloc.Heap = false
				}
			}
		}
	}
}

// ownGoMaxImplicitStack — межа gc для неявної алокації на стеку
// (ir.MaxImplicitStackVarSize).
const ownGoMaxImplicitStack = 64 << 10

// ownGoStackStatic переводить місця place.Stack розміром до межі gc на стек
// (P-NEW-04.2): без Heap AllocEx дає alloca у вхідному блоці й обнулення на
// місці, а місце лишає static і не отримує ні NoteAlloc, ні FreeAccounted.
func (p *context) ownGoStackStatic(place census.Placement) {
	for alloc := range place.Stack {
		fn := alloc.Parent()
		if fn.TypeParams().Len() > len(fn.TypeArgs()) {
			continue // тіло узагальненої функції: розміри типів невідомі
		}
		size := p.prog.SizeOf(p.type_(alloc.Type().(*types.Pointer).Elem(), llssa.GoDecl))
		if size == 0 || size > ownGoMaxImplicitStack {
			continue
		}
		alloc.Heap = false
		delete(place.StaticUses, alloc)
		place.Cats[alloc] = "stack"
	}
}

// ownGoConstLabel позначає місце MakeInterface сталої, чиє слово даних — незмінна
// глобальна змінна (P-NEW-04.4).
const ownGoConstLabel = "const"

// ownGoConstIfaces знімає з купи MakeInterface сталої в місці static: codegen
// бере слово даних з незмінної глобальної змінної (MakeInterfaceConst), і місце
// не має ні NoteAlloc, ні FreeAccounted. Інші категорії лишаються: Dec чи
// обхід звільнили б глобальну адресу.
func ownGoConstIfaces(place census.Placement) {
	for site, label := range place.Cats {
		mi, ok := site.(*ssa.MakeInterface)
		if !ok || isUntypedNilConst(mi.X) {
			continue
		}
		if _, ok := mi.X.(*ssa.Const); !ok {
			continue
		}
		if head, _, _ := strings.Cut(label, ":"); head != "static" {
			continue
		}
		delete(place.StaticUses, site)
		place.Cats[site] = ownGoConstLabel
	}
}

// ownGoLocalPun: кожне використання адреси — читання, запис або
// unsafe.Pointer, що одразу стає вказівником не більшого типу, яким лише
// читають і пишуть. Хоч одне таке перетворення є.
func (p *context) ownGoLocalPun(alloc *ssa.Alloc) bool {
	size := p.prog.SizeOf(p.type_(alloc.Type().(*types.Pointer).Elem(), llssa.GoDecl))
	punned := false
	for _, ref := range *alloc.Referrers() {
		if ownGoAccessOnly(alloc, ref) {
			continue
		}
		conv, ok := ref.(*ssa.Convert)
		if !ok {
			return false
		}
		if basic, ok := conv.Type().Underlying().(*types.Basic); !ok || basic.Kind() != types.UnsafePointer {
			return false
		}
		for _, use := range *conv.Referrers() {
			if _, ok := use.(*ssa.DebugRef); ok {
				continue
			}
			ptr, ok := use.(*ssa.Convert)
			if !ok {
				return false
			}
			typ, ok := ptr.Type().Underlying().(*types.Pointer)
			if !ok || p.prog.SizeOf(p.type_(typ.Elem(), llssa.GoDecl)) > size {
				return false
			}
			for _, access := range *ptr.Referrers() {
				if !ownGoAccessOnly(ptr, access) {
					return false
				}
			}
		}
		punned = true
	}
	return punned
}

// ownGoAccessOnly: instr читає чи пише за адресою addr, а саму адресу нікуди
// не передає.
func ownGoAccessOnly(addr ssa.Value, instr ssa.Instruction) bool {
	switch instr := instr.(type) {
	case *ssa.Store:
		return instr.Addr == addr && instr.Val != addr
	case *ssa.UnOp:
		return instr.Op == token.MUL && instr.X == addr
	case *ssa.DebugRef:
		return true
	}
	return false
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
func ownGoFreeAfter(fn *ssa.Function, uses map[ssa.Instruction][]ssa.Instruction, keepStored bool) (after map[ssa.Instruction][]ssa.Value, entry map[*ssa.BasicBlock][]ssa.Value, edge map[census.BlockEdge][]ssa.Value) {
	if fn == nil || len(fn.Blocks) == 0 || len(uses) == 0 {
		return nil, nil, nil
	}
	out := map[ssa.Instruction][]ssa.Value{}
	entry = map[*ssa.BasicBlock][]ssa.Value{}
	edge = map[census.BlockEdge][]ssa.Value{}
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
		lasts, entries, splits := deaths(fn, user, site)
		for _, block := range entries {
			if site.Block() != nil && site.Block().Dominates(block) {
				entry[block] = append(entry[block], val)
			}
		}
		for _, e := range splits {
			if site.Block() != nil && site.Block().Dominates(e.Pred) {
				edge[e] = append(edge[e], val)
			}
		}
		for instr := range lasts {
			// Використання аліаса в блоці, якого алокація не домінує,
			// не має місця для free: значення LLVM там недоступне.
			if instr.Block() == nil || site.Block() == nil || !site.Block().Dominates(instr.Block()) {
				continue
			}
			out[instr] = append(out[instr], val)
		}
	}
	if len(out) == 0 {
		out = nil
	}
	if len(entry) == 0 {
		entry = nil
	}
	if len(edge) == 0 {
		edge = nil
	}
	return out, entry, edge
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
func deaths(fn *ssa.Function, user map[ssa.Instruction]bool, site ssa.Instruction) (map[ssa.Instruction]struct{}, []*ssa.BasicBlock, []census.BlockEdge) {
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
	// Значення, живе на виході блока, вмирає на ребрі в наступника, де воно
	// вже мертве: останнє використання в циклі, перед раннім return чи
	// перед виходом з циклу через кілька break. Free стає на вхід
	// наступника, якщо значення живе на виході кожного його попередника,
	// інакше — на розщеплене ребро з кожного такого попередника.
	// Блок алокації сюди не йде: на його вході живе лише наступна алокація.
	var entries []*ssa.BasicBlock
	var splits []census.BlockEdge
	for _, succ := range fn.Blocks {
		if succ == nil || succ == def || liveIn[succ.Index] {
			continue
		}
		var live []*ssa.BasicBlock
		for _, pred := range succ.Preds {
			if liveOut[pred.Index] && !slices.Contains(live, pred) {
				live = append(live, pred)
			}
		}
		switch {
		case len(live) == 0:
		case len(live) == len(succ.Preds):
			entries = append(entries, succ)
		default:
			for _, pred := range live {
				splits = append(splits, census.BlockEdge{Pred: pred, Succ: succ})
			}
		}
	}
	return out, entries, splits
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
	if _, alloc := instr.(*ssa.Alloc); alloc && p.ownGoInRegionClone() {
		return // байти регіону облікує RegionAlloc
	}
	if p.ownGoAppendUnsized(instr) {
		return // NoteAlloc з 0 байтів нічого не облікує, а стоїть на кожному виклику
	}
	p.countOwnGoSite(b, val, "note_alloc")
	p.callOwnGoAccount(b, "NoteAlloc", llssa.Nil, p.ownGoSizeExpr(b, val), cat)
}

func (p *context) emitOwnGoFree(b llssa.Builder, instr ssa.Instruction) {
	p.emitOwnGoFreeSites(b, p.ownGoFreeAfter[instr])
	p.emitOwnGoDecSites(b, p.ownGoDecAfter[instr])
}

// emitOwnGoEntryFree звільняє місця, що вмерли на ребрі в block.
func (p *context) emitOwnGoEntryFree(b llssa.Builder, block *ssa.BasicBlock) {
	p.emitOwnGoFreeSites(b, p.ownGoFreeEntry[block])
	p.emitOwnGoDecSites(b, p.ownGoDecEntry[block])
}

// ownGoEdgeBlock — блок, куди веде ребро pred -> succ: сам succ або новий
// блок, що розщеплює ребро, якщо на ньому вмирає місце (P-NEW-03.3).
func (p *context) ownGoEdgeBlock(pred, succ *ssa.BasicBlock) llssa.BasicBlock {
	e := census.BlockEdge{Pred: pred, Succ: succ}
	if p.ownGoFreeEdge[e] == nil && p.ownGoDecEdge[e] == nil {
		return p.fn.Block(succ.Index)
	}
	if blk, ok := p.ownGoEdgeBlocks[e]; ok {
		return blk
	}
	if p.ownGoEdgeBlocks == nil {
		p.ownGoEdgeBlocks = map[census.BlockEdge]llssa.BasicBlock{}
	}
	blk := p.fn.MakeBlock()
	p.ownGoEdgeBlocks[e] = blk
	return blk
}

// emitOwnGoEdgeFree заповнює блоки ownGoEdgeBlock ребер з pred після його
// термінатора: звільнення і перехід у наступника.
func (p *context) emitOwnGoEdgeFree(b llssa.Builder, pred *ssa.BasicBlock) {
	for i, succ := range pred.Succs {
		e := census.BlockEdge{Pred: pred, Succ: succ}
		blk, ok := p.ownGoEdgeBlocks[e]
		if !ok || slices.Index(pred.Succs, succ) < i {
			continue
		}
		b.SetBlock(blk)
		p.emitOwnGoFreeSites(b, p.ownGoFreeEdge[e])
		p.emitOwnGoDecSites(b, p.ownGoDecEdge[e])
		b.Jump(p.fn.Block(succ.Index))
	}
}

func (p *context) emitOwnGoFreeSites(b llssa.Builder, sites []ssa.Value) {
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
		p.countOwnGoSite(b, site, "free_accounted")
		p.callOwnGoAccount(b, "FreeAccounted", ptr, size, cat)
	}
}

func (p *context) emitOwnGoDecSites(b llssa.Builder, sites []ssa.Value) {
	for _, site := range sites {
		if region, ok := p.ownGoRegionOf[site]; ok {
			p.countOwnGoSite(b, site, "free_region")
			p.callFreeRegion(b, region)
			continue
		}
		ptr := p.ownGoFreePtr(b, site)
		if ptr.IsNil() {
			continue
		}
		if _, sized := p.ownGoSiteSize(site); !sized {
			continue
		}
		if instr, ok := site.(ssa.Instruction); ok {
			if _, shared := p.ownGoShared[instr]; shared {
				p.countOwnGoSite(b, site, "dec_shared")
				p.callDecShared(b, ptr, p.sharedDesc(instr))
				continue
			}
		}
		if instr, ok := site.(ssa.Instruction); ok && p.ownGoCat[instr] == ownGoCyclic {
			p.countOwnGoSite(b, site, "dec_cyclic")
			p.callDecCyclic(b, ptr, ownGoElem(site))
			continue
		}
		p.countOwnGoSite(b, site, "dec_accounted")
		p.callOwnGoAccount(b, "DecAccounted", ptr, p.ownGoSizeExpr(b, site), 0)
	}
}

// emitOwnGoStoreInc рахує вказівник місця cyclic, записаний у поле обходу.
func (p *context) emitOwnGoStoreInc(b llssa.Builder, store *ssa.Store, val llssa.Expr) {
	sites := p.ownGoInc[store]
	if len(sites) == 0 || val.IsConstant() {
		return
	}
	// Записане значення може бути одним з кількох місць; рядок — першого.
	p.countOwnGoSite(b, sites[0], "inc")
	p.callOwnGoAccount(b, "Inc", val, llssa.Nil, 0)
}

// emitOwnGoIncShared ставить IncShared перед go на кожне місце, яке
// горутина отримує, і повертає ці місця для DecShared у рутині.
func (p *context) emitOwnGoIncShared(b llssa.Builder, g *ssa.Go) []ssa.Value {
	var sites []ssa.Value
	unsafePtr := types.Typ[types.UnsafePointer]
	sig := types.NewSignatureType(nil, nil, nil, types.NewTuple(
		types.NewVar(token.NoPos, nil, "ptr", unsafePtr),
	), nil, false)
	dst := p.type_(unsafePtr, llssa.GoDecl)
	for _, site := range p.ownGoGoShared[g] {
		ptr := p.ownGoFreePtr(b, site)
		if ptr.IsNil() {
			continue
		}
		p.countOwnGoSite(b, site, "inc_shared")
		b.Call(p.runtimeFunc("IncShared", sig), b.PtrCast(dst, ptr))
		sites = append(sites, site)
	}
	return sites
}

// ownGoRoutineDec дописує вказівники місць p.ownGoGoSites у стартовий запис
// горутини, а рутина після повернення функції горутини ставить на кожне
// DecShared і звільняє контекст замикання p.ownGoGoCtx, якщо він є.
func (p *context) ownGoRoutineDec(b llssa.Builder, buildCall func(llssa.Builder, llssa.Expr, ...llssa.Expr) llssa.Expr, args []llssa.Expr) (func(llssa.Builder, llssa.Expr, ...llssa.Expr) llssa.Expr, []llssa.Expr) {
	n := len(args)
	descs := make([]llssa.Expr, 0, len(p.ownGoGoSites))
	full := append([]llssa.Expr(nil), args...)
	for _, site := range p.ownGoGoSites {
		full = append(full, p.ownGoFreePtr(b, site))
		descs = append(descs, p.sharedDesc(site.(ssa.Instruction)))
	}
	sites := p.ownGoGoSites
	ctx := p.ownGoGoCtx
	wrapped := func(b llssa.Builder, fn llssa.Expr, all ...llssa.Expr) llssa.Expr {
		ret := buildCall(b, fn, all[:n]...)
		for i, ptr := range all[n:] {
			p.countOwnGoSite(b, sites[i], "dec_shared")
			p.callDecShared(b, ptr, descs[i])
		}
		if ctx != nil {
			// Байти контексту не обліковуються, як у static замикання.
			p.countOwnGoSite(b, ctx, "free_accounted")
			p.callOwnGoAccount(b, "FreeAccounted", b.Field(fn, 1), p.prog.IntVal(0, p.prog.Uintptr()), ownGoStatic)
		}
		return ret
	}
	return wrapped, full
}

func (p *context) callDecShared(b llssa.Builder, ptr, desc llssa.Expr) {
	unsafePtr := types.Typ[types.UnsafePointer]
	sig := types.NewSignatureType(nil, nil, nil, types.NewTuple(
		types.NewVar(token.NoPos, nil, "ptr", unsafePtr),
		types.NewVar(token.NoPos, nil, "desc", unsafePtr),
	), nil, false)
	dst := p.type_(unsafePtr, llssa.GoDecl)
	b.Call(p.runtimeFunc("DecShared", sig), b.PtrCast(dst, ptr), b.PtrCast(dst, desc))
}

// sharedDesc повертає глобальну runtime.SharedDesc місця спільної втечі:
// розмір об'єкта, категорію і ребро у вміст комірки за census.Shared.
// Розкладка збігається з runtime.SharedDesc і runtime.SharedEdge.
func (p *context) sharedDesc(site ssa.Instruction) llssa.Expr {
	if desc, ok := p.ownGoSharedDescs[site]; ok {
		return desc
	}
	if p.ownGoSharedDescs == nil {
		p.ownGoSharedDescs = map[ssa.Instruction]llssa.Expr{}
	}
	uintptrT, unsafePtr := types.Typ[types.Uintptr], types.Typ[types.UnsafePointer]
	edgeT := types.NewStruct([]*types.Var{
		types.NewField(token.NoPos, nil, "Off", uintptrT, false),
		types.NewField(token.NoPos, nil, "Shared", unsafePtr, false),
		types.NewField(token.NoPos, nil, "Cyclic", unsafePtr, false),
		types.NewField(token.NoPos, nil, "Size", uintptrT, false),
	}, nil)
	edgesT := types.NewSlice(edgeT)
	descT := types.NewStruct([]*types.Var{
		types.NewField(token.NoPos, nil, "Size", uintptrT, false),
		types.NewField(token.NoPos, nil, "Cat", types.Typ[types.Int32], false),
		types.NewField(token.NoPos, nil, "Edges", edgesT, false),
	}, nil)
	name := p.pkg.Path() + ".__owngo_shared$" + strconv.Itoa(len(p.ownGoSharedDescs))
	global := p.pkg.NewVarEx(name, p.prog.Pointer(p.type_(descT, llssa.GoDecl)))
	p.ownGoSharedDescs[site] = global.Expr

	edges := p.prog.Zero(p.type_(edgesT, llssa.GoDecl))
	if info := p.ownGoShared[site]; info.Content != nil {
		cyclicDesc := p.prog.Nil(p.type_(unsafePtr, llssa.GoDecl))
		size := p.prog.IntVal(0, p.prog.Uintptr())
		if info.ContentCyclic {
			cyclicDesc = p.cyclicDesc(info.Content)
		} else {
			size = p.prog.IntVal(p.prog.SizeOf(p.type_(info.Content, llssa.GoDecl)), p.prog.Uintptr())
		}
		edge := p.prog.ConstStruct(p.type_(edgeT, llssa.GoDecl), []llssa.Expr{
			p.prog.IntVal(0, p.prog.Uintptr()),
			p.prog.Nil(p.type_(unsafePtr, llssa.GoDecl)),
			cyclicDesc,
			size,
		})
		edges = p.pkg.ConstSlice(name+"$edges", p.type_(edgesT, llssa.GoDecl), []llssa.Expr{edge})
	}
	global.Init(p.prog.ConstStruct(p.type_(descT, llssa.GoDecl), []llssa.Expr{
		p.prog.IntVal(p.prog.SizeOf(p.type_(ownGoElem(site.(ssa.Value)), llssa.GoDecl)), p.prog.Uintptr()),
		p.prog.IntVal(p.ownGoCat[site], p.prog.Int32()),
		edges,
	}))
	return global.Expr
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
		p.countOwnGoSite(b, site, "inc")
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
	elem := ownGoElem(site)
	if elem == nil {
		return 0, false
	}
	n := p.prog.SizeOf(p.type_(elem, llssa.GoDecl))
	return n, n > 0
}

// ownGoElem повертає тип об'єкта місця: для свіжого внутрішнього результату —
// тип бази у викликаній функції, інакше — тип за вказівником місця.
func ownGoElem(site ssa.Value) types.Type {
	if call, ok := site.(*ssa.Call); ok {
		if base := census.InteriorBase(call); base != nil {
			return base
		}
	}
	ptr, ok := types.Unalias(site.Type()).Underlying().(*types.Pointer)
	if !ok {
		return nil
	}
	return ptr.Elem()
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
// ownGoAppendUnsized: append, буфер якого не підраховується і не
// звільняється, обліковує 0 байтів.
func (p *context) ownGoAppendUnsized(instr ssa.Instruction) bool {
	if census.ImplicitOrigin(instr) != "append" {
		return false
	}
	call, _ := instr.(*ssa.Call)
	_, counted := p.ownGoCounted[instr]
	return !counted && !p.ownGoWillFree(call)
}

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
	if p.ownGoAppendUnsized(instr) {
		return zero
	}
	switch underlying := site.Type().Underlying().(type) {
	case *types.Slice:
		// Буфер append має cap елементів (OwnedAppendCap), не len.
		elem := p.prog.SizeOf(p.type_(underlying.Elem(), llssa.GoDecl))
		return b.Convert(p.prog.Uintptr(), p.ownGoBytes(b, b.SliceCap(expr), elem))
	case *types.Basic:
		if underlying.Kind() == types.String {
			return b.Convert(p.prog.Uintptr(), b.StringLen(expr))
		}
	}
	return zero
}

// ownGoFreePtr повертає вказівник блока місця для входів володіння. Константа —
// адреса глобальної змінної, зокрема спільного символу купової алокації
// нульового розміру, — не блок купи: Nil.
func (p *context) ownGoFreePtr(b llssa.Builder, site ssa.Value) llssa.Expr {
	ptr := p.ownGoSitePtr(b, site)
	if ptr.IsConstant() {
		return llssa.Nil
	}
	return ptr
}

func (p *context) ownGoSitePtr(b llssa.Builder, site ssa.Value) llssa.Expr {
	expr, ok := p.bvals[site]
	if !ok || expr.IsNil() {
		return llssa.Nil
	}
	if ownGoFreshCall(site) {
		if census.InteriorBase(site.(*ssa.Call)) != nil {
			return p.ownGoInteriorPtr(b, expr)
		}
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

// ownGoInteriorPtr повертає вказівник користувача блока AllocCounted, усередині
// якого лежить expr: runtime.Base дає адресу слова, користувач — за словом.
func (p *context) ownGoInteriorPtr(b llssa.Builder, expr llssa.Expr) llssa.Expr {
	if _, ok := expr.Type.RawType().Underlying().(*types.Slice); ok {
		expr = b.SliceData(expr)
	}
	unsafePtr := types.Typ[types.UnsafePointer]
	sig := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(types.NewVar(token.NoPos, nil, "ptr", unsafePtr)),
		types.NewTuple(types.NewVar(token.NoPos, nil, "base", unsafePtr)),
		false)
	dst := p.type_(unsafePtr, llssa.GoDecl)
	word := b.Call(p.runtimeFunc("Base", sig), b.PtrCast(dst, expr))
	return b.Advance(word, p.prog.IntVal(p.prog.SizeOf(p.prog.Uintptr()), p.prog.Int()))
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
	intTy := types.Typ[types.Int]
	capSig := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(types.NewVar(token.NoPos, nil, "newLen", intTy), types.NewVar(token.NoPos, nil, "oldCap", intTy), types.NewVar(token.NoPos, nil, "etSize", intTy)),
		types.NewTuple(types.NewVar(token.NoPos, nil, "", intTy)),
		false)
	newCap := b.Call(p.runtimeFunc("OwnedAppendCap", capSig), newLen, b.SliceCap(src), p.prog.IntVal(elemSize, p.prog.Int()))
	raw := p.callSizedAlloc(b, allocName, p.ownGoBytes(b, newCap, elemSize))
	elemPtr := b.PtrCast(p.prog.Pointer(elemLL), raw)
	b.CopyBytes(elemPtr, b.SliceData(src), p.ownGoBytes(b, oldLen, elemSize))
	b.CopyBytes(b.Advance(elemPtr, oldLen), extraPtr, p.ownGoBytes(b, extraCount, elemSize))
	return b.SliceFromData(elemPtr, newLen, newCap)
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

// verifyOwnGo перевіряє census.Verify рішення володіння f до codegen:
// звільнення, які поставив ownGoFreeAfter, читачів місць і Inc (P-NEW-03.1).
func (p *context) verifyOwnGo(f *ssa.Function) {
	if !p.options.OwnGoFree {
		return
	}
	own := census.Ownership{
		Uses:  map[ssa.Instruction][]ssa.Instruction{},
		After: map[ssa.Instruction][]ssa.Value{},
		Entry: map[*ssa.BasicBlock][]ssa.Value{},
		Edge:  map[census.BlockEdge][]ssa.Value{},
		Inc:   p.ownGoInc,
	}
	addSites := func(sites []ssa.Value) {
		for _, site := range sites {
			if instr, ok := site.(ssa.Instruction); ok {
				own.Uses[instr] = slices.Concat(p.ownGoUses[instr], p.ownGoDecUses[instr])
			}
		}
	}
	for _, m := range []map[ssa.Instruction][]ssa.Value{p.ownGoFreeAfter, p.ownGoDecAfter} {
		for instr, sites := range m {
			own.After[instr] = append(own.After[instr], sites...)
			addSites(sites)
		}
	}
	for _, m := range []map[*ssa.BasicBlock][]ssa.Value{p.ownGoFreeEntry, p.ownGoDecEntry} {
		for block, sites := range m {
			own.Entry[block] = append(own.Entry[block], sites...)
			addSites(sites)
		}
	}
	for _, m := range []map[census.BlockEdge][]ssa.Value{p.ownGoFreeEdge, p.ownGoDecEdge} {
		for e, sites := range m {
			own.Edge[e] = append(own.Edge[e], sites...)
			addSites(sites)
		}
	}
	for _, e := range census.Verify(f, own) {
		p.ownGoErrs = append(p.ownGoErrs, fmt.Errorf("%v: owngo: %s: %w", p.fset.Position(e.Pos), f.String(), e))
	}
}
