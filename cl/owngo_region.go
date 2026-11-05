package cl

import (
	"go/token"
	"go/types"

	llssa "github.com/xgo-dev/llgo/ssa"
	"golang.org/x/tools/go/ssa"
)

// ownGoInRegionClone: зараз компілюється тіло клону F$region будівника.
func (p *context) ownGoInRegionClone() bool {
	return !p.ownGoRegionParam.IsNil()
}

// ownGoRegionFunc повертає клон F$region будівника f (P-NEW-05.4): регіон —
// перший параметр, далі отримувач і параметри f. Пакет будівника клон
// визначає, решта лише оголошує й кличе за іменем.
func (p *context) ownGoRegionFunc(f *ssa.Function) llssa.Function {
	_, name, ftype := p.funcName(f)
	name += "$region"
	if fn := p.pkg.FuncOf(name); fn != nil {
		return fn
	}
	oldGoFn := p.goFn
	p.goFn = f
	sig := p.patchType(f.Signature).(*types.Signature)
	p.goFn = oldGoFn
	params := []*types.Var{types.NewParam(token.NoPos, nil, "$region", types.Typ[types.UnsafePointer])}
	if recv := sig.Recv(); recv != nil {
		params = append(params, recv)
	}
	for param := range sig.Params().Variables() {
		params = append(params, param)
	}
	cloneSig := types.NewSignatureType(nil, nil, nil, types.NewTuple(params...), sig.Results(), sig.Variadic())
	return p.pkg.NewFuncEx(name, cloneSig, llssa.CallConv(ftype), false, p.needsLinkOnce(f))
}

// ownGoRegionCall повертає регіон для виклику: корінь (P-NEW-05.2) створює
// новий, а виклик будівника в клоні передає регіон клону далі.
func (p *context) ownGoRegionCall(b llssa.Builder, call *ssa.Call) (llssa.Expr, bool) {
	if p.ownGoInRegionClone() {
		return p.ownGoRegionParam, p.ownGoBuilders[call.Call.StaticCallee()]
	}
	if _, root := p.ownGoRegions[call]; !root {
		return llssa.Nil, false
	}
	// Без точки смерті (корінь читає defer) FreeRegion нікуди поставити, а
	// незвільнений регіон тримав би цілий шматок замість кількох вузлів.
	if _, dies := p.ownGoDecUses[call]; !dies {
		return llssa.Nil, false
	}
	p.countOwnGoSite(b, call, "region_create")
	sig := types.NewSignatureType(nil, nil, nil, nil,
		types.NewTuple(types.NewVar(token.NoPos, nil, "region", types.Typ[types.UnsafePointer])), false)
	region := b.Call(p.runtimeFunc("RegionCreate", sig))
	if p.ownGoRegionOf == nil {
		p.ownGoRegionOf = map[ssa.Value]llssa.Expr{}
	}
	p.ownGoRegionOf[call] = region
	return region, true
}

// callOwnGoRegion кличе клон будівника з регіоном першим аргументом.
func (p *context) callOwnGoRegion(b llssa.Builder, call *ssa.Call, region llssa.Expr) llssa.Expr {
	clone := p.ownGoRegionFunc(call.Call.StaticCallee())
	args := append([]llssa.Expr{region}, p.compileValues(b, call.Call.Args, fnNormal)...)
	return b.Call(clone.Expr, args...)
}

// emitRegionAlloc виділяє вузол у регіоні клону: пам'ять уже обнулена, слова
// лічильника немає.
func (p *context) emitRegionAlloc(b llssa.Builder, elem llssa.Type) llssa.Expr {
	unsafePtr := types.Typ[types.UnsafePointer]
	sig := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(
			types.NewVar(token.NoPos, nil, "region", unsafePtr),
			types.NewVar(token.NoPos, nil, "size", types.Typ[types.Uintptr])),
		types.NewTuple(types.NewVar(token.NoPos, nil, "ptr", unsafePtr)),
		false)
	raw := b.Call(p.runtimeFunc("RegionAlloc", sig), p.ownGoRegionParam, p.prog.IntVal(p.prog.SizeOf(elem), p.prog.Uintptr()))
	return b.PtrCast(p.prog.Pointer(elem), raw)
}

func (p *context) callFreeRegion(b llssa.Builder, region llssa.Expr) {
	sig := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(types.NewVar(token.NoPos, nil, "region", types.Typ[types.UnsafePointer])), nil, false)
	b.Call(p.runtimeFunc("FreeRegion", sig), region)
}
