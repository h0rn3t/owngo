package cl_test

import (
	"testing"

	"github.com/xgo-dev/llgo/cl/cltest"
	"github.com/xgo-dev/llgo/internal/build"
)

// TestOwnGoInteriorBytes — P8.6: фікстура сама звіряє зміну owngo freed і
// owngo live rc-local за P3.5 і виходить з кодом 0, лише якщо обидва збіглися.
func TestOwnGoInteriorBytes(t *testing.T) {
	for _, fixture := range []string{
		"interior",      // локальний &s.f (P8.4): 16 байтів звільнено
		"freshinterior", // свіжий внутрішній результат (P8.5): звільняє викликач
		"interiorheld",  // невідомий виклик тримає &s.f: 16 байтів живі
	} {
		t.Run(fixture, func(t *testing.T) {
			conf := build.NewDefaultConf(build.ModeRun)
			conf.Tags = "owngo"
			out, err := cltest.RunAndCaptureWithConf("../owngo/bytes/"+fixture, "", conf)
			if err != nil {
				t.Errorf("llgo run -tags owngo ./owngo/bytes/%s error = %v, want exit 0\n%s", fixture, err, out)
			}
		})
	}
}

// TestOwnGoSharedBytes — P9.4: фікстури самі звіряють Freed і Live місць
// спільної втечі через go.
func TestOwnGoSharedBytes(t *testing.T) {
	for _, fixture := range []string{
		"sharedgo",     // змінна в замиканні go: звільнена після Wait і кінця функції
		"sharedtree",   // горутина пише NewTree у змінну: звільнені комірка і дерево
		"sharedglobal", // те саме місце ще в глобальній змінній: живе
	} {
		t.Run(fixture, func(t *testing.T) {
			conf := build.NewDefaultConf(build.ModeRun)
			conf.Tags = "owngo"
			out, err := cltest.RunAndCaptureWithConf("../owngo/bytes/"+fixture, "", conf)
			if err != nil {
				t.Errorf("llgo run -tags owngo ./owngo/bytes/%s error = %v, want exit 0\n%s", fixture, err, out)
			}
		})
	}
}

// TestOwnGoPinnedBytes — P10.4: закріплене місце в межах функції, через межу
// виклику і в полі звільненого батька лишається серед живих; фікстура сама
// звіряє Freed, Live(pinned) і суму живих.
func TestOwnGoPinnedBytes(t *testing.T) {
	conf := build.NewDefaultConf(build.ModeRun)
	conf.Tags = "owngo"
	out, err := cltest.RunAndCaptureWithConf("../owngo/bytes/pinned", "", conf)
	if err != nil {
		t.Errorf("llgo run -tags owngo ./owngo/bytes/pinned error = %v, want exit 0\n%s", err, out)
	}
}

// TestOwnGoPinnedMapKey — P11.0: присвоєння, читання і видалення в мапі з
// рядковим ключем і ключем-вказівником не додають байтів до live pinned.
func TestOwnGoPinnedMapKey(t *testing.T) {
	conf := build.NewDefaultConf(build.ModeRun)
	conf.Tags = "owngo"
	out, err := cltest.RunAndCaptureWithConf("../owngo/bytes/mapkey", "", conf)
	if err != nil {
		t.Errorf("llgo run -tags owngo ./owngo/bytes/mapkey error = %v, want exit 0\n%s", err, out)
	}
}

// TestOwnGoPinnedTypePun — P11.0.1: type-pun у межах функції (math.Float64bits,
// strconv, код модуля) не додає байтів до live pinned; адреса, що втікає в
// глобальну змінну, лишається pinned.
func TestOwnGoPinnedTypePun(t *testing.T) {
	conf := build.NewDefaultConf(build.ModeRun)
	conf.Tags = "owngo"
	out, err := cltest.RunAndCaptureWithConf("../owngo/bytes/typepun", "", conf)
	if err != nil {
		t.Errorf("llgo run -tags owngo ./owngo/bytes/typepun error = %v, want exit 0\n%s", err, out)
	}
}
