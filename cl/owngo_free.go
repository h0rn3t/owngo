package cl

import (
	"go/token"
	"go/types"
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
	cats, uses := census.Sites(funcs)
	p.ownGoUses = uses
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
func ownGoFreeAfter(fn *ssa.Function, uses map[ssa.Instruction][]ssa.Instruction) map[ssa.Instruction][]ssa.Value {
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
		if pointerStored(val, users) {
			continue
		}
		user := map[ssa.Instruction]bool{}
		for _, instr := range users {
			if instr != nil && instr.Parent() == fn {
				user[instr] = true
			}
		}
		for instr := range deaths(fn, user) {
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

func deaths(fn *ssa.Function, user map[ssa.Instruction]bool) map[ssa.Instruction]struct{} {
	n := len(fn.Blocks)
	used := make([]bool, n)
	for instr := range user {
		block := instr.Block()
		if block == nil || block.Parent() != fn || block.Index < 0 || block.Index >= n {
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
			in := used[i] || out
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
		if block == nil || !liveIn[block.Index] || liveOut[block.Index] {
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
	size, ok := p.ownGoSiteSize(val)
	if !ok {
		return
	}
	p.callOwnGoAccount(b, "NoteAlloc", llssa.Nil, size, cat)
}

func (p *context) emitOwnGoFree(b llssa.Builder, instr ssa.Instruction) {
	sites := p.ownGoFreeAfter[instr]
	if len(sites) == 0 {
		return
	}
	for _, site := range sites {
		ptr := p.ownGoFreePtr(b, site)
		if ptr.IsNil() {
			continue
		}
		size, sized := p.ownGoSiteSize(site)
		cat, known := ownGoStatic, false
		if siteInstr, isInstr := site.(ssa.Instruction); isInstr {
			cat, known = p.ownGoCat[siteInstr]
		}
		if !sized || !known {
			size = 0
			cat = ownGoStatic
		}
		p.callOwnGoAccount(b, "FreeAccounted", ptr, size, cat)
	}
}

func (p *context) ownGoSiteSize(site ssa.Value) (uint64, bool) {
	alloc, ok := site.(*ssa.Alloc)
	if !ok || !alloc.Heap {
		return 0, false
	}
	ptr, ok := types.Unalias(alloc.Type()).Underlying().(*types.Pointer)
	if !ok {
		return 0, false
	}
	n := p.prog.SizeOf(p.type_(ptr.Elem(), llssa.GoDecl))
	return n, n > 0
}

func (p *context) callOwnGoAccount(b llssa.Builder, name string, ptr llssa.Expr, size uint64, cat uint64) {
	params := []*types.Var{
		types.NewVar(token.NoPos, nil, "size", types.Typ[types.Uintptr]),
		types.NewVar(token.NoPos, nil, "cat", types.Typ[types.Int32]),
	}
	args := []llssa.Expr{
		p.prog.IntVal(size, p.prog.Uintptr()),
		p.prog.IntVal(cat, p.prog.Int32()),
	}
	if name == "FreeAccounted" {
		params = append([]*types.Var{
			types.NewVar(token.NoPos, nil, "ptr", types.Typ[types.UnsafePointer]),
		}, params...)
		dst := p.type_(types.Typ[types.UnsafePointer], llssa.GoDecl)
		args = append([]llssa.Expr{b.PtrCast(dst, ptr)}, args...)
	}
	sig := types.NewSignatureType(nil, nil, nil, types.NewTuple(params...), nil, false)
	b.Call(p.runtimeFunc(name, sig), args...)
}

func (p *context) ownGoFreePtr(b llssa.Builder, site ssa.Value) llssa.Expr {
	expr, ok := p.bvals[site]
	if !ok || expr.IsNil() {
		return llssa.Nil
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
		return llssa.Nil
	}
}
