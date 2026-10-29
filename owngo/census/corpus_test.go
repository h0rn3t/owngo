package census

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestCorpus ганяє фікстури testdata/src/corpus через analysistest
// і звіряє текст категорії v0.
//
// Діагностика — рівно один рядок на місце, без префікса:
//
//	stack
//	static
//	rc-local: call|closure|return
//	rc-shared: go|channel|global
//	cyclic
//	cyclic: go|channel|global|closure|return|call
//	pinned: unsafe
//	implicit: append|concat|string
//
// Місця (після ssa.Package.Build, уже з lift):
//   - кожен *ssa.Alloc, що лишився в тілі
//   - *ssa.MakeSlice, *ssa.MakeMap, *ssa.MakeChan
//   - *ssa.MakeClosure з непорожнім Bindings
//   - *ssa.MakeInterface, якщо конкретний тип не вказівник
//   - виклик builtin append
//   - *ssa.BinOp додавання рядків
//   - *ssa.Convert між string і []byte або []rune
//
// Pos() інструкції. У MakeClosure Pos часто невалідний: тоді позиція —
// ключове слово func літерала (Pos функції SSA).
//
// Категорія, перша що спрацювала:
//  1. pinned — адреса місця або внутрішній вказівник від нього в цій функції
//     перетворюється на unsafe.Pointer, включно з uintptr(unsafe.Pointer(...)).
//  2. stack — *ssa.Alloc з Heap == false.
//  3. cyclic — тип місця (після зняття одного вказівника) досягає себе через
//     поля-вказівники, елементи слайса чи масиву або значення мапи.
//     Поле-структура обходиться далі. Діагностика v0 — строгий режим:
//     поле-інтерфейс ребра не дає. Консервативний режим (ребро до кожного
//     іменованого типу цього пакета, який можна присвоїти інтерфейсу)
//     вмикає прапор -conservative. Типова діагностика лишається строгою. Канал, функція і ключ мапи не обходяться.
//  4. static — вказівник не втікає з функції.
//  5. rc-shared — втеча go, channel або global.
//  6. rc-local — будь-яка інша втеча.
//
// Втеча всередині однієї функції, без міжпроцедурного розбору. Запис у об'єкт
// наслідує втечу цього об'єкта; запис у об'єкт, який сам не втікає, втечею не є.
// Непрозорий виклик (функція, метод, інтерфейс, func-значення) — причина call,
// навіть якщо тіло калі порожнє. Не є ним builtin-и, індексація, поля,
// розіменування, операції мапи й каналу над самим контейнером і виклик, де
// значення — це callee, а не аргумент.
//
// Причина сильнішої втечі: go, channel, global, closure, return, call.
// Замикання, яке запускають через go, дає go, не closure.
// Перезапис глобальної змінної попередню втечу не скасовує.
// Надсилання в канал є channel, навіть якщо канал не залишає функцію.
// implicit не проходить каскад: причина — вид операції, не втеча.
// Тимчасовий масив varargs у append — окреме місце Alloc, не implicit.
// Захоплення вказівника замиканням дає друге місце Alloc (*T) на рядку new.
func TestCorpus(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "corpus")
}

// quiet ковтає звірку текстів analysistest. TestSiteCount звіряє лише місця.
type quiet struct{}

func (quiet) Errorf(string, ...any) {}

// TestSiteCount перевіряє, що кількість діагностик на рядку дорівнює кількості «// want».
// Текст категорії звіряє TestCorpus.
func TestSiteCount(t *testing.T) {
	results := analysistest.Run(quiet{}, analysistest.TestData(), Analyzer, "corpus")
	if len(results) != 1 || results[0].Err != nil {
		var err error
		if len(results) == 1 {
			err = results[0].Err
		}
		t.Fatalf("corpus results = %d, err = %v", len(results), err)
	}
	got := map[string]int{}
	for _, d := range results[0].Diagnostics {
		pos := results[0].Pass.Fset.Position(d.Pos)
		key := filepath.Base(pos.Filename) + ":" + strconv.Itoa(pos.Line)
		got[key]++
	}
	want := wantLines(t)
	if maps.Equal(got, want) {
		return
	}
	keys := slices.Collect(maps.Keys(got))
	for k := range want {
		if _, ok := got[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	for _, k := range keys {
		if got[k] != want[k] {
			t.Errorf("sites %s = %d, want %d", k, got[k], want[k])
		}
	}
}

func wantLines(t *testing.T) map[string]int {
	t.Helper()
	dir := filepath.Join(analysistest.TestData(), "src", "corpus")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s) error = %v", dir, err)
	}
	counts := map[string]int{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("ReadFile(%s) error = %v", entry.Name(), err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			n := quotedWants(line)
			if n == 0 {
				continue
			}
			counts[entry.Name()+":"+strconv.Itoa(i+1)] = n
		}
	}
	return counts
}

func quotedWants(line string) int {
	i := strings.Index(line, "// want")
	if i < 0 {
		return 0
	}
	rest := line[i+len("// want"):]
	n := 0
	for i := 0; i < len(rest); i++ {
		if rest[i] != '"' && rest[i] != '`' {
			continue
		}
		quote := rest[i]
		i++
		for i < len(rest) && rest[i] != quote {
			if rest[i] == '\\' && quote == '"' {
				i++
			}
			i++
		}
		n++
	}
	return n
}
