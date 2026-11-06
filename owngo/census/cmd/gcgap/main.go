// Команда gcgap зважує розрив стека go/ssa і gc на програмі (P-NEW-04.1).
//
// Вхід — каталог Go-програми і stderr її збірки під owngostat: рядки
// `owngo site` з операціями alloc і alloc_counted дають число алокацій місця.
// Для кожного місця gcgap знаходить купові об'єкти go/ssa на тому самому
// file:line, а `go build -gcflags=all=-m` каже, що gc робить саме з цим
// об'єктом: «escapes to heap» чи «moved to heap» — heap, «does not escape» —
// gap. Масив аргументів append gc не створює зовсім, тож це завжди gap.
// Місце без позиції, у рантаймі llgo чи без діагностики — none.
//
// Правило рядка з owngo/GC-GAP.md (census.StackGaps) — поруч, у рядку strict:
// воно не розрізняє об'єкти одного рядка, і «append escapes to heap» для
// росту зрізу ховає масив аргументів того самого append.
//
// Використання: gcgap -dir програма -stat файл [-root корінь llgo]
package main

import (
	"bytes"
	"cmp"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"

	"github.com/xgo-dev/llgo/owngo/census"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("gcgap", flag.ContinueOnError)
	dir := fs.String("dir", "", "program directory")
	stat := fs.String("stat", "", "stderr of an owngostat run")
	root := fs.String("root", ".", "llgo root: relative site paths resolve against it")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" || *stat == "" || fs.NArg() != 0 {
		return errors.New("usage: gcgap -dir program -stat file [-root llgo]")
	}
	text, err := os.ReadFile(*stat)
	if err != nil {
		return err
	}
	rows, opsAlloc, err := readSites(text)
	if err != nil {
		return err
	}
	absDir, err := filepath.Abs(*dir)
	if err != nil {
		return err
	}
	absRoot, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	gcText, err := gcOutput(absDir)
	if err != nil {
		return err
	}
	gaps, err := gapRows(absDir, absRoot, rows, gcText)
	if err != nil {
		return err
	}
	diags, err := census.ParseGCText(strings.NewReader(gcText))
	if err != nil {
		return err
	}

	var buf []byte
	var total int64
	byVerdict := map[string]int64{}
	byKind := map[string]int64{}
	for _, g := range gaps {
		total += g.N
		byVerdict[g.Verdict] += g.N
		if g.Verdict == "gap" {
			byKind[g.Kind] += g.N
		}
		buf = fmt.Appendf(buf, "site\t%d\t%s\t%s\t%s\t%s\t%s\n", g.N, g.Verdict, g.Kind, g.Category, g.Reason, g.Pos)
	}
	// Правило рядка GC-GAP.md: місце census з абсолютним шляхом і його число.
	strictN := map[census.Site]int64{}
	var sites []census.Site
	for _, r := range rows {
		s := census.Site{Category: r.Category, Pos: resolve(absRoot, r.Pos)}
		strictN[s] += r.N
		sites = append(sites, s)
	}
	var strict int64
	for _, s := range census.StackGaps(sites, diags) {
		strict += strictN[s]
		strictN[s] = 0 // рядок з кількома причинами одного місця рахується раз
	}
	buf = fmt.Appendf(buf, "total\t%d\tops_alloc\t%d\n", total, opsAlloc)
	for _, v := range []string{"gap", "heap", "mixed", "none"} {
		buf = fmt.Appendf(buf, "verdict\t%s\t%d\n", v, byVerdict[v])
	}
	kinds := slices.SortedFunc(maps.Keys(byKind), func(a, b string) int { return cmp.Compare(byKind[b], byKind[a]) })
	for _, k := range kinds {
		buf = fmt.Appendf(buf, "gap\t%s\t%d\n", k, byKind[k])
	}
	buf = fmt.Appendf(buf, "strict\t%d\n", strict)
	_, err = stdout.Write(buf)
	return err
}

// siteRow — алокації одного рядка `owngo site`: alloc і alloc_counted разом,
// бо обидві йдуть через AllocU і обидві входять в `owngo ops alloc`.
type siteRow struct {
	Pos, Category, Reason string
	N                     int64
}

// readSites повертає рядки місць з алокаціями в порядку файла і `owngo ops alloc`.
func readSites(text []byte) ([]siteRow, int64, error) {
	var rows []siteRow
	index := map[siteRow]int{}
	var opsAlloc int64
	for line := range strings.Lines(string(text)) {
		f := strings.Fields(line)
		if len(f) == 4 && f[0] == "owngo" && f[1] == "ops" && f[2] == "alloc" {
			n, err := strconv.ParseInt(f[3], 10, 64)
			if err != nil {
				return nil, 0, fmt.Errorf("%q: %w", line, err)
			}
			opsAlloc = n
			continue
		}
		if len(f) != 7 || f[0] != "owngo" || f[1] != "site" || (f[5] != "alloc" && f[5] != "alloc_counted") {
			continue
		}
		n, err := strconv.ParseInt(f[6], 10, 64)
		if err != nil {
			return nil, 0, fmt.Errorf("%q: %w", line, err)
		}
		key := siteRow{Pos: f[2], Category: f[3], Reason: f[4]}
		if i, ok := index[key]; ok {
			rows[i].N += n
			continue
		}
		index[key] = len(rows)
		key.N = n
		rows = append(rows, key)
	}
	return rows, opsAlloc, nil
}

// gapRow — місце з видом купових об'єктів go/ssa і вердиктом gc.
// Kind — види об'єктів рядка через «+»; Verdict — gap, heap, mixed або none.
type gapRow struct {
	siteRow
	Kind, Verdict string
}

// heapObj — куповий об'єкт go/ssa: вид і, для змінної, її ім'я.
type heapObj struct{ kind, name string }

// gapRows ставить кожному місцю вид і вердикт gc; gcText — вивід gcOutput(dir).
func gapRows(dir, root string, rows []siteRow, gcText string) ([]gapRow, error) {
	objects, err := heapObjects(dir)
	if err != nil {
		return nil, err
	}
	msgs := map[string][]string{}
	for line := range strings.Lines(gcText) {
		m := gcLineRE.FindStringSubmatch(strings.TrimSuffix(line, "\n"))
		if m == nil {
			continue
		}
		file := m[1]
		if !filepath.IsAbs(file) {
			file = filepath.Join(dir, file)
		}
		key := file + ":" + m[2]
		msgs[key] = append(msgs[key], m[3])
	}

	out := make([]gapRow, 0, len(rows))
	for _, r := range rows {
		g := gapRow{siteRow: r, Kind: "-", Verdict: "none"}
		if strings.HasPrefix(r.Pos, ":") {
			g.Kind = "nopos"
			out = append(out, g)
			continue
		}
		key := resolve(root, r.Pos)
		objs := objects[key]
		if len(objs) == 0 {
			out = append(out, g)
			continue
		}
		var kinds, verdicts []string
		for _, o := range objs {
			kinds = append(kinds, o.kind)
			verdicts = append(verdicts, verdict(o, msgs[key]))
		}
		slices.Sort(kinds)
		g.Kind = strings.Join(slices.Compact(kinds), "+")
		slices.Sort(verdicts)
		verdicts = slices.Compact(verdicts)
		g.Verdict = verdicts[0]
		if len(verdicts) > 1 {
			g.Verdict = "mixed"
		}
		out = append(out, g)
	}
	return out, nil
}

var gcLineRE = regexp.MustCompile(`^(.+):(\d+):\d+: (.*)$`)

// subjects — початок тексту gc для об'єкта кожного виду.
var subjects = map[string][]string{
	"new":       {"new("},
	"complit":   {"&"},
	"slicelit":  {"[]"},
	"makeslice": {"make([]"},
	"makemap":   {"make(map", "map["},
	"closure":   {"func literal"},
	"args":      {"... argument"},
}

// verdict — що gc робить з об'єктом за повідомленнями його рядка.
func verdict(o heapObj, msgs []string) string {
	switch o.kind {
	case "append-args":
		return "gap" // gc пише аргументи одразу в backing array, окремого масиву немає
	case "makechan":
		return "heap" // runtime.makechan завжди в купі
	case "var":
		if slices.Contains(msgs, "moved to heap: "+o.name) {
			return "heap"
		}
		return "gap"
	}
	prefixes := subjects[o.kind]
	got := "none"
	for _, msg := range msgs {
		subject, heap := strings.CutSuffix(msg, " escapes to heap")
		if !heap {
			var ok bool
			if subject, ok = strings.CutSuffix(msg, " does not escape"); !ok {
				continue
			}
		}
		if !slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(subject, p) }) {
			continue
		}
		if heap {
			return "heap"
		}
		got = "gap"
	}
	return got
}

// heapObjects — купові об'єкти go/ssa програми і всіх її залежностей
// за абсолютним file:line, з позицією, як її пише таблиця місць llgo.
func heapObjects(dir string) (map[string][]heapObj, error) {
	pkgs, err := packages.Load(&packages.Config{Mode: packages.LoadAllSyntax, Dir: dir}, ".")
	if err != nil {
		return nil, err
	}
	if packages.PrintErrors(pkgs) > 0 {
		return nil, errors.New("go/packages: помилки завантаження")
	}
	prog, _ := ssautil.AllPackages(pkgs, ssa.InstantiateGenerics)
	prog.Build()

	out := map[string][]heapObj{}
	for fn := range ssautil.AllFunctions(prog) {
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				v, ok := instr.(ssa.Value)
				if !ok {
					continue
				}
				obj, ok := heapObject(v)
				if !ok {
					continue
				}
				pos := v.Pos()
				if c, ok := v.(*ssa.MakeClosure); ok && !pos.IsValid() {
					pos = c.Fn.Pos() // як cl.ownGoSiteText
				}
				if !pos.IsValid() {
					continue
				}
				p := prog.Fset.Position(pos)
				key := p.Filename + ":" + strconv.Itoa(p.Line)
				if !slices.Contains(out[key], obj) {
					out[key] = append(out[key], obj)
				}
			}
		}
	}
	return out, nil
}

func heapObject(v ssa.Value) (heapObj, bool) {
	switch v := v.(type) {
	case *ssa.Alloc:
		if !v.Heap {
			return heapObj{}, false
		}
		switch v.Comment {
		case "new", "complit", "slicelit", "makeslice":
			return heapObj{kind: v.Comment}, true
		case "varargs":
			if appendArgs(v) {
				return heapObj{kind: "append-args"}, true
			}
			return heapObj{kind: "args"}, true
		}
		return heapObj{kind: "var", name: v.Comment}, true
	case *ssa.MakeClosure:
		return heapObj{kind: "closure"}, true
	case *ssa.MakeSlice:
		return heapObj{kind: "makeslice"}, true
	case *ssa.MakeMap:
		return heapObj{kind: "makemap"}, true
	case *ssa.MakeChan:
		return heapObj{kind: "makechan"}, true
	case *ssa.MakeInterface:
		return heapObj{kind: "iface"}, true
	}
	return heapObj{}, false
}

// appendArgs — масив variadic-аргументів, чий зріз іде у builtin append.
func appendArgs(a *ssa.Alloc) bool {
	for _, ref := range *a.Referrers() {
		s, ok := ref.(*ssa.Slice)
		if !ok {
			continue
		}
		for _, use := range *s.Referrers() {
			call, ok := use.(*ssa.Call)
			if !ok {
				continue
			}
			if b, ok := call.Call.Value.(*ssa.Builtin); ok && b.Name() == "append" {
				return true
			}
		}
	}
	return false
}

// gcOutput — stderr `go build -gcflags=all=-m` програми в dir.
func gcOutput(dir string) (string, error) {
	cmd := exec.Command("go", "build", "-gcflags=all=-m", "-o", os.DevNull, ".") //nolint:gosec // G204: fixed program and argv
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("go build -m: %w\n%s", err, stderr.String())
	}
	return stderr.String(), nil
}

// resolve — абсолютний file:line місця; шлях рантайму llgo — від кореня llgo.
func resolve(root, pos string) string {
	if strings.HasPrefix(pos, ":") || filepath.IsAbs(pos) {
		return pos
	}
	return filepath.Join(root, pos)
}
