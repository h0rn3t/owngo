package census

import "golang.org/x/tools/go/ssa"

// StaticUses повідомляє місця категорії static за правилами v1 і інструкції,
// які їх читають. Виклик функції поза funcs невідомий, тож таке місце не static.
// Порожній funcs дає nil.
func StaticUses(funcs []*ssa.Function) map[ssa.Instruction][]ssa.Instruction {
	if len(funcs) == 0 {
		return nil
	}
	ctx := &v1Ctx{sums: packageSummaries(nil, funcs)}
	out := make(map[ssa.Instruction][]ssa.Instruction)
	for _, fn := range funcs {
		if fn == nil || len(fn.Blocks) == 0 {
			continue
		}
		msg, pts := messages(fn, ifaceStrict, nil, ctx)
		for site, text := range msg {
			if text != "static" {
				continue
			}
			users := readers(site, pts)
			if len(users) > 0 {
				out[site] = users
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
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
