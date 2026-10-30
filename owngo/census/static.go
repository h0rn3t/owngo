package census

import (
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
	if len(funcs) == 0 {
		return nil, nil, nil, nil, nil
	}
	ctx := &v1Ctx{sums: packageSummaries(nil, funcs)}
	cats = map[ssa.Instruction]string{}
	staticUses = map[ssa.Instruction][]ssa.Instruction{}
	decUses = map[ssa.Instruction][]ssa.Instruction{}
	counted = map[ssa.Instruction]struct{}{}
	for _, fn := range funcs {
		if fn == nil || len(fn.Blocks) == 0 {
			continue
		}
		msg, pts, contents := messages(fn, ifaceStrict, nil, ctx)
		for site, label := range msg {
			cats[site] = label
			head, _, _ := strings.Cut(label, ":")
			if head == "static" {
				users := siteUsers(site, pts, contents)
				if len(users) > 0 {
					staticUses[site] = users
				}
				continue
			}
			if !countedAlloc(site, label) && !countedImplicit(site, label) {
				continue
			}
			counted[site] = struct{}{}
			if !decSite(site, label, pts, ctx.noDec) {
				continue
			}
			users := rcUsers(siteUsers(site, pts, contents))
			if len(users) > 0 {
				decUses[site] = users
			}
		}
	}
	inc = filterInc(ctx.inc, counted)
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
	return cats, staticUses, decUses, counted, inc
}

func countedAlloc(site ssa.Instruction, label string) bool {
	alloc, ok := site.(*ssa.Alloc)
	if !ok || !alloc.Heap {
		return false
	}
	head, _, _ := strings.Cut(label, ":")
	return head == "rc-local"
}

func decSite(site ssa.Instruction, label string, pts map[ssa.Value]siteSet, noDec map[ssa.Instruction]bool) bool {
	if noDec[site] || interiorPoints(site, pts) {
		return false
	}
	_, reason, _ := strings.Cut(label, ": ")
	switch reason {
	case "return", "move", "call":
		return false
	default:
		return true
	}
}

func countedImplicit(site ssa.Instruction, label string) bool {
	if !CountableImplicit(site) {
		return false
	}
	head, _, _ := strings.Cut(label, ":")
	return head == "rc-local"
}

// siteUsers додає читачів контейнера, у contents якого лежить це місце.
// Замикання тримає alloc змінної, а не саму конкатенацію: без цього Dec
// став би на Store і звільнив буфер до виклику замикання.
func siteUsers(site ssa.Instruction, pts map[ssa.Value]siteSet, contents map[ssa.Instruction]siteSet) []ssa.Instruction {
	users := readers(site, pts)
	if ImplicitOrigin(site) == "" {
		return users
	}
	for obj, inner := range contents {
		if _, ok := inner[site]; !ok {
			continue
		}
		users = append(users, readers(obj, pts)...)
	}
	return users
}

func rcUsers(users []ssa.Instruction) []ssa.Instruction {
	extra, leaves := closureUses(users)
	if leaves {
		return nil
	}
	if len(extra) == 0 {
		return users
	}
	return append(users, extra...)
}

func closureUses(users []ssa.Instruction) (extra []ssa.Instruction, leaves bool) {
	seen := map[ssa.Instruction]bool{}
	for _, user := range users {
		seen[user] = true
	}
	for _, user := range users {
		clo, ok := user.(*ssa.MakeClosure)
		if !ok || clo.Referrers() == nil {
			continue
		}
		for _, ref := range *clo.Referrers() {
			if ref == nil {
				continue
			}
			switch ref := ref.(type) {
			case *ssa.Return, *ssa.Go, *ssa.Send:
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

func interiorPoints(site ssa.Instruction, pts map[ssa.Value]siteSet) bool {
	for val, set := range pts {
		if _, ok := set[site]; !ok {
			continue
		}
		if interiorPointer(val) {
			return true
		}
	}
	return false
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
