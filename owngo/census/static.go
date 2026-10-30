package census

import "golang.org/x/tools/go/ssa"

// Sites класифікує кожне місце за правилами v1 (строгий режим інтерфейсів)
// і повертає читачів місць static. Виклик поза funcs невідомий, тож таке
// місце не static. Порожній funcs дає nil, nil.
func Sites(funcs []*ssa.Function) (map[ssa.Instruction]string, map[ssa.Instruction][]ssa.Instruction) {
	if len(funcs) == 0 {
		return nil, nil
	}
	ctx := &v1Ctx{sums: packageSummaries(nil, funcs)}
	cats := make(map[ssa.Instruction]string)
	uses := make(map[ssa.Instruction][]ssa.Instruction)
	for _, fn := range funcs {
		if fn == nil || len(fn.Blocks) == 0 {
			continue
		}
		msg, pts := messages(fn, ifaceStrict, nil, ctx)
		for site, text := range msg {
			cats[site] = text
			if text != "static" {
				continue
			}
			users := readers(site, pts)
			if len(users) > 0 {
				uses[site] = users
			}
		}
	}
	if len(cats) == 0 {
		cats = nil
	}
	if len(uses) == 0 {
		uses = nil
	}
	return cats, uses
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
