package ssa

import (
	"go/token"
	"go/types"
	"testing"

	"github.com/goplus/gogen/packages"
)

// TestModuleZeroSentinelDeclaredVar перевіряє модуль, який і оголошує
// спільний символ нульового розміру змінною (linkname рантайму owngo), і
// алокує нульовий розмір: в модулі один глобал з цим ім'ям, і він визначений,
// за будь-якого порядку.
func TestModuleZeroSentinelDeclaredVar(t *testing.T) {
	tests := []struct {
		name     string
		varFirst bool
	}{
		{"varFirst", true},
		{"zeroFirst", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prog := NewProgram(&Target{GOOS: "linux", GOARCH: "amd64"})
			defer prog.Dispose()
			prog.SetRuntime(func() *types.Package {
				pkg, _ := packages.NewImporter(token.NewFileSet()).Import(PkgRuntime)
				return pkg
			})
			pkg := prog.NewPackage("bar", "foo/bar")
			declare := func() Global {
				return pkg.NewVar(moduleZeroName, types.NewPointer(types.Typ[types.Uint8]), GoDecl)
			}
			allocZero := func() {
				pkg.NewVar("foo/bar.a", types.NewPointer(types.NewArray(types.Typ[types.Int], 0)), GoDecl)
			}
			var sentinel Global
			if tt.varFirst {
				sentinel = declare()
				allocZero()
			} else {
				allocZero()
				sentinel = declare()
			}

			gbl := pkg.mod.NamedGlobal(moduleZeroName)
			if gbl.Initializer().IsNil() {
				t.Errorf("%s: %s is a declaration, want a definition", tt.name, moduleZeroName)
			}
			if sentinel.impl != gbl {
				t.Errorf("%s: NewVar(%s) = %s, want the module sentinel", tt.name, moduleZeroName, sentinel.impl.Name())
			}
		})
	}
}
