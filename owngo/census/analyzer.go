package census

import (
	"reflect"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/buildssa"
)

// Analyzer знаходить місця алокації пакета і звітує unknown.
// Текст категорій із корпусу з'явиться в P0.4.
var Analyzer = &analysis.Analyzer{
	Name:       "census",
	Doc:        "класифікує місця алокації Go-програми",
	Requires:   []*analysis.Analyzer{buildssa.Analyzer},
	Run:        run,
	ResultType: reflect.TypeFor[struct{}](),
}

func run(pass *analysis.Pass) (any, error) {
	return struct{}{}, nil
}
