package census

import (
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"
)

// TestCorpus ганяє фікстури testdata/src/corpus через analysistest.
// Заглушка нічого не звітує, тож тест компілюється і падає, доки немає аналізатора.
//
// P0.3 замінює заглушку на census.Analyzer і звіряє кількість місць із кількістю
// «// want». P0.4 має зробити текст діагностик таким, як у фікстурах.
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
//     поля-вказівники, елементи слайса або значення мапи. Поле-структура
//     обходиться далі. Поле-інтерфейс у v0 ребро не дає (строгий режим);
//     консервативний режим — задача P0.5. Канал за елементом не обходиться.
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
	stub := &analysis.Analyzer{
		Name: "census",
		Doc:  "заглушка корпусу census; аналізатор з'явиться в P0.3",
		Run: func(*analysis.Pass) (any, error) {
			return nil, nil
		},
	}
	analysistest.Run(t, analysistest.TestData(), stub, "corpus")
}
