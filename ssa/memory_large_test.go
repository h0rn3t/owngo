package ssa

import (
	"go/token"
	"go/types"
	"strings"
	"testing"

	llabi "github.com/xgo-dev/llgo/internal/abi"
)

func TestAllocLargeLocalOnHeap(t *testing.T) {
	prog := NewProgram(nil)
	defer prog.Dispose()
	runtimePkg := types.NewPackage(PkgRuntime, "runtime")
	params := types.NewTuple(types.NewVar(token.NoPos, runtimePkg, "size", types.Typ[types.Uintptr]))
	results := types.NewTuple(types.NewVar(token.NoPos, runtimePkg, "", types.Typ[types.UnsafePointer]))
	allocZ := types.NewFunc(token.NoPos, runtimePkg, "AllocZ", types.NewSignatureType(nil, nil, nil, params, results, false))
	runtimePkg.Scope().Insert(allocZ)
	prog.SetRuntime(runtimePkg)

	pkg := prog.NewPackage("main", "main")
	b := pkg.NewFunc("main", NoArgsNoRet, GoDecl).MakeBody(1)
	small := prog.Type(types.NewArray(types.Typ[types.Byte], int64(llabi.MaxStackVarSize)), GoDecl)
	large := prog.Type(types.NewArray(types.Typ[types.Byte], int64(llabi.MaxStackVarSize+1)), GoDecl)
	b.Alloc(small, false)
	b.Alloc(large, false)
	b.Return()
	b.EndBuild()

	ir := pkg.String()
	if !strings.Contains(ir, "alloca [131072 x i8]") {
		t.Fatalf("local at the explicit stack limit was not kept on the stack:\n%s", ir)
	}
	if strings.Contains(ir, "alloca [131073 x i8]") {
		t.Fatalf("local above the explicit stack limit remained on the stack:\n%s", ir)
	}
	if !strings.Contains(ir, `call ptr @"github.com/xgo-dev/llgo/runtime/internal/runtime.AllocZ"(i64 131073)`) {
		t.Fatalf("large local was not allocated with runtime.AllocZ:\n%s", ir)
	}
	if !pkg.NeedRuntime {
		t.Fatal("large local heap allocation did not mark the runtime as needed")
	}
}

// TestStoreLargeZeroUsesMemset — P-NEW-09.2: запис нульового агрегата у 64 КіБ
// (brotli: h.tiny_hash = [65536]byte{}) як store [65536 x i8] zeroinitializer
// розгортав SelectionDAG на 65 536 вузлів, і бекенд LLVM падав переповненням
// стека. Великий нуль іде через llvm.memset, малий лишається store.
func TestStoreLargeZeroUsesMemset(t *testing.T) {
	prog := NewProgram(nil)
	defer prog.Dispose()
	pkg := prog.NewPackage("p", "example.com/zerostore")
	store := func(name string, n int64, volatile bool) string {
		arr := types.NewArray(types.Typ[types.Byte], n)
		sig := types.NewSignatureType(nil, nil, nil, types.NewTuple(
			types.NewVar(token.NoPos, nil, "p", types.NewPointer(arr)),
		), nil, false)
		fn := pkg.NewFunc(name, sig, GoDecl)
		b := fn.MakeBody(1)
		st := b.Store(fn.Param(0), prog.Zero(prog.Type(arr, GoDecl)))
		if volatile {
			st.SetVolatile(true)
		}
		b.Return()
		b.EndBuild()
		return fn.impl.String()
	}

	if ir := store("large", 65536, false); strings.Contains(ir, "store [65536 x i8]") || !strings.Contains(ir, "@llvm.memset") || !strings.Contains(ir, "i64 65536, i1 false)") {
		t.Errorf("Store([65536]byte{}) = want llvm.memset of 65536 bytes, no aggregate store:\n%s", ir)
	}
	if ir := store("largeVolatile", 65536, true); !strings.Contains(ir, "i64 65536, i1 true)") {
		t.Errorf("Store([65536]byte{}).SetVolatile(true) = want a volatile llvm.memset:\n%s", ir)
	}
	if ir := store("small", 16, false); !strings.Contains(ir, "store [16 x i8] zeroinitializer") || strings.Contains(ir, "@llvm.memset") {
		t.Errorf("Store([16]byte{}) = want a plain store:\n%s", ir)
	}
}
