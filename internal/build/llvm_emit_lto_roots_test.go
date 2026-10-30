package build

import (
	"strings"
	"testing"

	"github.com/xgo-dev/llgo/internal/crosscompile"
	"github.com/xgo-dev/llgo/internal/lto"
	"github.com/xgo-dev/llvm"
)

func testCompilerUsedNames(t *testing.T, mod llvm.Module) []string {
	t.Helper()
	used := mod.NamedGlobal("llvm.compiler.used")
	if used.IsNil() {
		return nil
	}
	var names []string
	init := used.Initializer()
	for i := 0; i < init.OperandsCount(); i++ {
		op := init.Operand(i)
		// Elements are bitcasts of the globals; resolve through them.
		if name := op.Name(); name != "" {
			names = append(names, name)
		} else if op.OperandsCount() > 0 {
			names = append(names, op.Operand(0).Name())
		}
	}
	return names
	return names
}

func TestPreserveZeroInitAggregatesForLTO(t *testing.T) {
	newModule := func(t *testing.T) llvm.Module {
		t.Helper()
		lc := llvm.NewContext()
		t.Cleanup(lc.Dispose)
		mod := lc.NewModule("preserve")
		t.Cleanup(mod.Dispose)
		i8ptr := llvm.PointerType(lc.Int8Type(), 0)
		structTy := llvm.StructType([]llvm.Type{i8ptr, i8ptr}, false)
		add := func(name string, ty llvm.Type, init llvm.Value, constant bool) {
			g := llvm.AddGlobal(mod, ty, name)
			g.SetInitializer(init)
			g.SetGlobalConstant(constant)
			g.SetAlignment(8)
		}
		nullStruct := llvm.ConstNull(structTy)
		add("zero_struct", structTy, nullStruct, false)
		ext := llvm.AddFunction(mod, "ext", llvm.FunctionType(i8ptr, nil, false))
		nonzero := llvm.ConstStruct([]llvm.Value{llvm.ConstBitCast(ext, i8ptr), llvm.ConstNull(i8ptr)}, false)
		add("init_struct", structTy, nonzero, false)
		add("zero_scalar", lc.Int64Type(), llvm.ConstNull(lc.Int64Type()), false)
		add("zero_const", structTy, nullStruct, true)
		tls := llvm.AddGlobal(mod, structTy, "zero_tls")
		tls.SetInitializer(nullStruct)
		tls.SetThreadLocal(true)
		return mod
	}
	conf := func(mode lto.Mode) *Config {
		return &Config{LTO: mode}
	}
	coff := crosscompile.NativeToolchain{ObjectFormat: crosscompile.ObjectFormatCOFF}
	elf := crosscompile.NativeToolchain{ObjectFormat: crosscompile.ObjectFormatELF}
	macho := crosscompile.NativeToolchain{ObjectFormat: crosscompile.ObjectFormatMachO}
	none := crosscompile.NativeToolchain{}

	for _, tc := range []struct {
		name string
		mode lto.Mode
		tool crosscompile.NativeToolchain
		want []string
	}{
		{"coff full", lto.Full, coff, []string{"zero_struct"}},
		{"coff thin", lto.Thin, coff, []string{"zero_struct"}},
		{"elf full", lto.Full, elf, []string{"zero_struct"}},
		{"off", lto.Off, coff, nil},
		{"macho", lto.Full, macho, nil},
		{"unknown", lto.Full, none, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mod := newModule(t)
			preserveZeroInitAggregatesForLTO(conf(tc.mode), tc.tool, mod)
			got := testCompilerUsedNames(t, mod)
			if len(got) != len(tc.want) {
				t.Fatalf("llvm.compiler.used = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("llvm.compiler.used = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestPreserveZeroInitAggregatesKeepsExisting(t *testing.T) {
	lc := llvm.NewContext()
	defer lc.Dispose()
	mod := lc.NewModule("preserve-existing")
	defer mod.Dispose()
	i8ptr := llvm.PointerType(lc.Int8Type(), 0)
	structTy := llvm.StructType([]llvm.Type{i8ptr, i8ptr}, false)
	g := llvm.AddGlobal(mod, structTy, "zero_struct")
	g.SetInitializer(llvm.ConstNull(structTy))
	keep := llvm.AddFunction(mod, "keep", llvm.FunctionType(lc.VoidType(), nil, false))
	used := llvm.AddGlobal(mod, llvm.ArrayType(i8ptr, 1), "llvm.compiler.used")
	used.SetInitializer(llvm.ConstArray(i8ptr, []llvm.Value{llvm.ConstBitCast(keep, i8ptr)}))
	used.SetLinkage(llvm.AppendingLinkage)

	conf := &Config{LTO: lto.Full}
	preserveZeroInitAggregatesForLTO(conf, crosscompile.NativeToolchain{ObjectFormat: crosscompile.ObjectFormatCOFF}, mod)
	got := testCompilerUsedNames(t, mod)
	foundStruct, foundKeep := false, false
	for _, n := range got {
		if strings.Contains(n, "zero_struct") {
			foundStruct = true
		}
		if n == "keep" {
			foundKeep = true
		}
	}
	if !foundStruct || !foundKeep {
		t.Fatalf("llvm.compiler.used = %v, want both zero_struct and keep", got)
	}
}
