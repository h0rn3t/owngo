package census

import (
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"
)

// TestV1 перевіряє модель передачі вказівників: позичання, переміщення, спільне.
// Корпус v0 лишається на причині call. Тут окремі фікстури.
func TestV1(t *testing.T) {
	enableV1(t)
	analysistest.Run(t, analysistest.TestData(), Analyzer, "v1", "v1user")
}

func enableV1(t *testing.T) {
	t.Helper()
	if Analyzer.Flags.Lookup("v1") == nil {
		t.Fatal("прапор v1 не зареєстровано")
	}
	if err := Analyzer.Flags.Set("v1", "true"); err != nil {
		t.Fatalf("Flags.Set(v1) error = %v", err)
	}
	Analyzer.FactTypes = []analysis.Fact{(*Summary)(nil)}
	t.Cleanup(func() {
		Analyzer.FactTypes = nil
		if err := Analyzer.Flags.Set("v1", "false"); err != nil {
			t.Errorf("Flags.Set(v1) error = %v", err)
		}
	})
}
