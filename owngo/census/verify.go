package census

import (
	"fmt"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// Ownership — рішення codegen про місця однієї функції: читачі місця
// (Placement.StaticUses і DecUses), звільнення (FreeAccounted чи Dec*)
// після інструкції, на вході блока чи на розщепленому ребрі і Inc на
// передачі чи записі.
type Ownership struct {
	Uses  map[ssa.Instruction][]ssa.Instruction
	After map[ssa.Instruction][]ssa.Value
	Entry map[*ssa.BasicBlock][]ssa.Value
	Edge  map[BlockEdge][]ssa.Value
	Inc   map[ssa.Instruction][]ssa.Value
}

// BlockEdge — ребро CFG з Pred у Succ.
type BlockEdge struct{ Pred, Succ *ssa.BasicBlock }

// OwnershipError — порушення володіння: що сталося з місцем Site у позиції Pos
// і номери блоків шляху від визначення місця до порушення.
type OwnershipError struct {
	Kind string
	Site ssa.Value
	Pos  token.Pos
	Path []int
}

func (v OwnershipError) Error() string {
	site := "<nil>"
	if v.Site != nil {
		site = v.Site.Name() + " = " + v.Site.String()
	}
	path := make([]string, len(v.Path))
	for i, b := range v.Path {
		path[i] = fmt.Sprintf("b%d", b)
	}
	return fmt.Sprintf("%s of %s on path %s", v.Kind, site, strings.Join(path, " -> "))
}

// Verify перевіряє рішення codegen для fn до codegen: на жодному шляху від
// місця немає двох звільнень і використання після звільнення, а кожен Inc
// має пару — Dec власника в цій функції чи поле обходу, куди пишуть
// вказівник. Шлях паніки не перевіряється: паніка лишає місце живим.
func Verify(fn *ssa.Function, own Ownership) []OwnershipError {
	if fn == nil || len(fn.Blocks) == 0 {
		return nil
	}
	released := map[ssa.Value]bool{}
	var sites []ssa.Instruction
	add := func(vals []ssa.Value) {
		for _, v := range vals {
			if instr, ok := v.(ssa.Instruction); ok && instr.Parent() == fn && !released[v] {
				sites = append(sites, instr)
			}
			released[v] = true
		}
	}
	for _, vals := range own.After {
		add(vals)
	}
	for _, vals := range own.Entry {
		add(vals)
	}
	for _, vals := range own.Edge {
		add(vals)
	}
	slices.SortFunc(sites, compareInstr)
	var out []OwnershipError
	for _, site := range sites {
		out = append(out, verifySite(fn, site, own)...)
	}
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			for _, val := range own.Inc[instr] {
				if released[val] || traversalStore(instr) {
					continue
				}
				out = append(out, OwnershipError{
					Kind: "inc without pair",
					Site: val,
					Pos:  instr.Pos(),
					Path: blockPath(val, b),
				})
			}
		}
	}
	return out
}

// siteState — стан місця на вході чи виході блока за всіма шляхами:
// live — на якомусь шляху об'єкт живий, released — на якомусь звільнений.
type siteState struct{ live, released bool }

func (s siteState) or(o siteState) siteState {
	return siteState{s.live || o.live, s.released || o.released}
}

// verifySite — прямий аналіз потоку даних по CFG. Кожне виконання site
// створює новий об'єкт, тож на site стан стає живим, а звільнення робить
// його звільненим на всіх шляхах через цю точку.
func verifySite(fn *ssa.Function, site ssa.Instruction, own Ownership) []OwnershipError {
	val := site.(ssa.Value)
	users := map[ssa.Instruction]bool{}
	for _, u := range own.Uses[site] {
		if u.Parent() == fn {
			users[u] = true
		}
	}
	afterCount := map[ssa.Instruction]int{}
	for instr, vals := range own.After {
		afterCount[instr] = countVal(vals, val)
	}
	in := make([]siteState, len(fn.Blocks))
	out := make([]siteState, len(fn.Blocks))
	var found []OwnershipError
	// edge — стан на ребрі p -> b після звільнень на ньому; double —
	// звільнення на ребрі, де місце вже звільнене.
	edge := func(p, b *ssa.BasicBlock) (st siteState, double bool) {
		st = out[p.Index]
		for range countVal(own.Edge[BlockEdge{p, b}], val) {
			double = double || st.released
			st = siteState{released: true}
		}
		return st, double
	}
	enter := func(b *ssa.BasicBlock, report bool) siteState {
		var st siteState
		for _, p := range b.Preds {
			e, double := edge(p, b)
			if double && report {
				found = append(found, OwnershipError{"double release", val, blockPos(b), blockPath(val, p)})
			}
			st = st.or(e)
		}
		return st
	}
	transfer := func(b *ssa.BasicBlock, report bool) siteState {
		st := in[b.Index]
		release := func(pos token.Pos) {
			if st.released && report {
				found = append(found, OwnershipError{"double release", val, pos, blockPath(val, b)})
			}
			st = siteState{released: true}
		}
		for range countVal(own.Entry[b], val) {
			release(blockPos(b))
		}
		for _, instr := range b.Instrs {
			if phi, ok := instr.(*ssa.Phi); ok {
				// Читач phi бере значення на ребрі: стан — на виході попередника.
				if report && users[phi] && phiReadsReleased(phi, val, users, edge) {
					found = append(found, OwnershipError{"use after release", val, phi.Pos(), blockPath(val, b)})
				}
				continue
			}
			if instr == site {
				st = siteState{live: true}
			} else if users[instr] && st.released && report {
				found = append(found, OwnershipError{"use after release", val, instr.Pos(), blockPath(val, b)})
			}
			for range afterCount[instr] {
				release(instr.Pos())
			}
		}
		return st
	}
	for changed := true; changed; {
		changed = false
		for _, b := range fn.Blocks {
			in[b.Index] = enter(b, false)
			if next := transfer(b, false); next != out[b.Index] {
				out[b.Index] = next
				changed = true
			}
		}
	}
	for _, b := range fn.Blocks {
		enter(b, true)
		transfer(b, true)
	}
	return found
}

// phiReadsReleased: ребро phi, що несе місце чи значення його читача,
// приходить з попередника, де місце на якомусь шляху вже звільнене, —
// в ньому чи на самому ребрі.
func phiReadsReleased(phi *ssa.Phi, site ssa.Value, users map[ssa.Instruction]bool, edge func(p, b *ssa.BasicBlock) (siteState, bool)) bool {
	for i, v := range phi.Edges {
		instr, _ := v.(ssa.Instruction)
		if v != site && !users[instr] {
			continue
		}
		if st, _ := edge(phi.Block().Preds[i], phi.Block()); st.released {
			return true
		}
	}
	return false
}

// traversalStore: instr пише вказівник у поле, яке обходить DecCyclic.
func traversalStore(instr ssa.Instruction) bool {
	store, ok := instr.(*ssa.Store)
	if !ok {
		return false
	}
	field, ok := store.Addr.(*ssa.FieldAddr)
	if !ok {
		return false
	}
	ptr, ok := field.X.Type().Underlying().(*types.Pointer)
	return ok && slices.Contains(CyclicFields(ptr.Elem()), field.Field)
}

func countVal(vals []ssa.Value, v ssa.Value) int {
	n := 0
	for _, x := range vals {
		if x == v {
			n++
		}
	}
	return n
}

// blockPath — найкоротший шлях блоків від блока місця до to; місце поза
// функцією дає лише to.
func blockPath(site ssa.Value, to *ssa.BasicBlock) []int {
	instr, ok := site.(ssa.Instruction)
	if !ok || instr.Block() == nil {
		return []int{to.Index}
	}
	from := instr.Block()
	prev := map[*ssa.BasicBlock]*ssa.BasicBlock{from: nil}
	queue := []*ssa.BasicBlock{from}
	for len(queue) > 0 && to != from {
		b := queue[0]
		queue = queue[1:]
		for _, s := range b.Succs {
			if _, seen := prev[s]; !seen {
				prev[s] = b
				queue = append(queue, s)
			}
		}
	}
	if _, ok := prev[to]; !ok {
		return []int{to.Index}
	}
	var path []int
	for b := to; b != nil; b = prev[b] {
		path = append(path, b.Index)
	}
	slices.Reverse(path)
	return path
}

func blockPos(b *ssa.BasicBlock) token.Pos {
	for _, instr := range b.Instrs {
		if pos := instr.Pos(); pos.IsValid() {
			return pos
		}
	}
	return token.NoPos
}

func compareInstr(a, b ssa.Instruction) int {
	if a.Block().Index != b.Block().Index {
		return a.Block().Index - b.Block().Index
	}
	return slices.Index(a.Block().Instrs, a) - slices.Index(b.Block().Instrs, b)
}
