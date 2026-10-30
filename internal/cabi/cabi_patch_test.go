package cabi

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	llssa "github.com/xgo-dev/llgo/ssa"
	"github.com/xgo-dev/llvm"
)

func TestTransformModuleSkipsLLVMScalableIntrinsic(t *testing.T) {
	const childEnv = "LLGO_CABI_SCALABLE_INTRINSIC_TEST_CHILD"
	if os.Getenv(childEnv) != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestTransformModuleSkipsLLVMScalableIntrinsic$")
		cmd.Env = append(os.Environ(), childEnv+"=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("C ABI transformer crashed on an LLVM scalable intrinsic: %v\n%s", err, output)
		}
		return
	}

	const testIR = `
declare void @llvm.test.scalable(<vscale x 2 x i64>)
declare void @llvm.test.metadata(metadata)

define void @probe() {
entry:
  call void @llvm.test.scalable(<vscale x 2 x i64> poison)
  call void @llvm.test.metadata(metadata !0)
  ret void
}

!0 = !{}
`
	ctx := llvm.NewContext()
	defer ctx.Dispose()
	path := filepath.Join(t.TempDir(), "scalable-intrinsic.ll")
	if err := os.WriteFile(path, []byte(testIR), 0o644); err != nil {
		t.Fatal(err)
	}
	buf, err := llvm.NewMemoryBufferFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	mod, err := ctx.ParseIR(buf)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Dispose()

	llvm.InitializeAllTargets()
	llvm.InitializeAllTargetMCs()
	llvm.InitializeAllTargetInfos()
	prog := llssa.NewProgram(&llssa.Target{GOOS: "darwin", GOARCH: "arm64"})
	defer prog.Dispose()
	NewTransformer(prog, "aarch64-apple-darwin", "", true).TransformModule("test", mod)
	if mod.NamedFunction("probe").IsNil() {
		t.Fatal("probe function disappeared")
	}
}

func TestShouldSkipLLVMIntrinsic(t *testing.T) {
	transformer := &Transformer{}
	for _, name := range []string{"llvm.test.scalable", "llvm.test.metadata"} {
		if !transformer.shouldSkipFunc(name) {
			t.Fatalf("LLVM intrinsic %q must bypass C ABI sizing", name)
		}
	}
	for _, name := range []string{"ordinary.function", "example.com/myllvm.Helper"} {
		if transformer.shouldSkipFunc(name) {
			t.Fatalf("ordinary function %q was skipped", name)
		}
	}
}

func TestTargetArchAndNewTransformerArchSelection(t *testing.T) {
	if got := targetArch("riscv64-unknown-linux-gnu"); got != "riscv64" {
		t.Fatalf("targetArch(triple) = %q, want riscv64", got)
	}
	if got := targetArch("thumbv7em-none-eabi"); got != "arm" {
		t.Fatalf("targetArch(thumb triple) = %q, want arm", got)
	}
	if got := targetArch("wasm"); got != "wasm" {
		t.Fatalf("targetArch(single arch) = %q, want wasm", got)
	}

	llvm.InitializeAllTargets()
	llvm.InitializeAllTargetMCs()
	llvm.InitializeAllTargetInfos()

	prog := llssa.NewProgram(nil)
	defer prog.Dispose()
	tests := []struct {
		target string
		abi    string
		arch   string
		check  func(TypeInfoSys) bool
	}{
		{"xtensa-esp32-none-elf", "", "xtensa", func(sys TypeInfoSys) bool { _, ok := sys.(*TypeInfoEsp32); return ok }},
		{"riscv32-unknown-elf", "ilp32f", "riscv32", func(sys TypeInfoSys) bool {
			rv, ok := sys.(*TypeInfoRiscv32)
			return ok && rv.mabi == "ilp32f"
		}},
		{"x86_64-unknown-linux-gnu", "", "amd64", func(sys TypeInfoSys) bool { _, ok := sys.(*TypeInfoAmd64); return ok }},
		{"aarch64-apple-darwin", "", "arm64", func(sys TypeInfoSys) bool { _, ok := sys.(*TypeInfoArm64); return ok }},
		{"arm-unknown-linux-gnueabihf", "", "arm", func(sys TypeInfoSys) bool { _, ok := sys.(*TypeInfoArm); return ok }},
		{"wasm32-unknown-wasip1", "", "wasm", func(sys TypeInfoSys) bool { _, ok := sys.(*TypeInfoWasm); return ok }},
		{"riscv64-unknown-linux-gnu", "lp64d", "riscv64", func(sys TypeInfoSys) bool {
			rv, ok := sys.(*TypeInfoRiscv64)
			return ok && rv.mabi == "lp64d"
		}},
		{"i386-unknown-linux-gnu", "", "386", func(sys TypeInfoSys) bool { _, ok := sys.(*TypeInfo386); return ok }},
	}
	for _, tc := range tests {
		tr := NewTransformer(prog, tc.target, tc.abi, true)
		if tr.arch != tc.arch {
			t.Fatalf("NewTransformer(%q).arch = %q, want %q", tc.target, tr.arch, tc.arch)
		}
		if !tr.optimize {
			t.Fatal("NewTransformer did not preserve optimize")
		}
		if !tc.check(tr.sys) {
			t.Fatalf("NewTransformer(%q) selected unexpected sys implementation %T", tc.target, tr.sys)
		}
	}
}

func checkTypeInfo(t *testing.T, tr *Transformer, typ llvm.Type, index int, kind AttrKind, type1 string) *TypeInfo {
	t.Helper()
	ftyp := llvm.FunctionType(typ.Context().VoidType(), nil, false)
	info := tr.GetTypeInfo(typ.Context(), ftyp, typ, index)
	if info.Kind != kind || info.Type1.String() != type1 {
		t.Fatalf("GetTypeInfo(%s, index %d) = kind %v, type %s; want kind %v, type %s",
			typ, index, info.Kind, info.Type1, kind, type1)
	}
	return info
}

func TestDevLTOGlobalDCEFuncNoUnwindCreatesNounwindAttribute(t *testing.T) {
	ctx := llvm.NewContext()
	attr := funcNoUnwind(ctx)
	if attr.IsNil() {
		t.Fatal("funcNoUnwind returned nil attribute")
	}
	if got, want := attr.GetEnumKind(), int(llvm.AttributeKindID("nounwind")); got != want {
		t.Fatalf("funcNoUnwind kind = %d, want %d", got, want)
	}
	if got := attr.GetEnumValue(); got != 0 {
		t.Fatalf("funcNoUnwind value = %d, want 0", got)
	}
}

func TestClosureEnvAttributeRemappedByCABI(t *testing.T) {
	llvm.InitializeAllTargets()
	llvm.InitializeAllTargetMCs()
	llvm.InitializeAllTargetInfos()

	const testIR = `
%Value = type { ptr, ptr, i64 }

define %Value @callee(ptr %g, ptr %out, ptr nest %env, %Value %value) {
entry:
  ret %Value %value
}

define %Value @caller(ptr %g, ptr %out, ptr nest %env, %Value %value) {
entry:
  %result = call %Value @callee(ptr %g, ptr %out, ptr nest %env, %Value %value)
  ret %Value %result
}
`
	ctx := llvm.NewContext()
	defer ctx.Dispose()
	path := filepath.Join(t.TempDir(), "closure_env.ll")
	if err := os.WriteFile(path, []byte(testIR), 0o644); err != nil {
		t.Fatal(err)
	}
	buf, err := llvm.NewMemoryBufferFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	mod, err := ctx.ParseIR(buf)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Dispose()

	prog := llssa.NewProgram(&llssa.Target{GOOS: "linux", GOARCH: "amd64"})
	defer prog.Dispose()
	tr := NewTransformer(prog, "amd64-unknown-linux-gnu", "", true)
	tr.TransformModule("test", mod)

	nest := llvm.AttributeKindID("nest")
	callee := mod.NamedFunction("callee")
	if attr := callee.GetEnumAttributeAtIndex(4, nest); attr.IsNil() {
		t.Fatalf("C ABI lowering lost/remapped nest on the definition:\n%s", callee.String())
	}
	if attr := callee.GetEnumAttributeAtIndex(3, nest); !attr.IsNil() {
		t.Fatalf("C ABI lowering left nest on the old definition parameter:\n%s", callee.String())
	}

	caller := mod.NamedFunction("caller")
	var nestedCall llvm.Value
	for block := caller.FirstBasicBlock(); !block.IsNil(); block = llvm.NextBasicBlock(block) {
		for instruction := block.FirstInstruction(); !instruction.IsNil(); instruction = llvm.NextInstruction(instruction) {
			if call := instruction.IsACallInst(); !call.IsNil() && call.CalledValue().Name() == "callee" {
				nestedCall = call
			}
		}
	}
	if nestedCall.IsNil() {
		t.Fatalf("transformed caller has no callee call:\n%s", caller.String())
	}
	if attr := nestedCall.GetCallSiteEnumAttribute(4, nest); attr.IsNil() {
		t.Fatalf("C ABI lowering lost/remapped nest on the call:\n%s", caller.String())
	}
	if attr := nestedCall.GetCallSiteEnumAttribute(3, nest); !attr.IsNil() {
		t.Fatalf("C ABI lowering left nest on the old call parameter:\n%s", caller.String())
	}
	if err := llvm.VerifyModule(mod, llvm.ReturnStatusAction); err != nil {
		t.Fatalf("C ABI closure-env module is invalid: %v\n%s", err, mod.String())
	}
}

func TestClosureEnvAttributesPreservedByCABI(t *testing.T) {
	llvm.InitializeAllTargets()
	llvm.InitializeAllTargetMCs()
	llvm.InitializeAllTargetInfos()

	const testIR = `
%Value = type { ptr, ptr, i64 }

define RETURN @callback(ptr ATTR %env, %Value %value) {
entry:
  RET
}
`
	returnCases := []struct {
		name string
		typ  string
		ret  string
	}{
		{name: "aggregate", typ: "%Value", ret: "ret %Value %value"},
		{name: "void", typ: "void", ret: "ret void"},
		{name: "scalar", typ: "i64", ret: "ret i64 7"},
	}
	for _, returnCase := range returnCases {
		t.Run(returnCase.name, func(t *testing.T) {
			for _, attrName := range []string{"nest", "swiftself"} {
				t.Run(attrName, func(t *testing.T) {
					ctx := llvm.NewContext()
					defer ctx.Dispose()
					path := filepath.Join(t.TempDir(), "closure_env.ll")
					ir := strings.NewReplacer(
						"RETURN", returnCase.typ,
						"RET", returnCase.ret,
						"ATTR", attrName,
					).Replace(testIR)
					if err := os.WriteFile(path, []byte(ir), 0o644); err != nil {
						t.Fatal(err)
					}
					buf, err := llvm.NewMemoryBufferFromFile(path)
					if err != nil {
						t.Fatal(err)
					}
					mod, err := ctx.ParseIR(buf)
					if err != nil {
						t.Fatal(err)
					}
					defer mod.Dispose()

					prog := llssa.NewProgram(&llssa.Target{GOOS: "linux", GOARCH: "amd64"})
					defer prog.Dispose()
					NewTransformer(prog, "amd64-unknown-linux-gnu", "", true).TransformModule("test", mod)

					callback := mod.NamedFunction("callback")
					kind := llvm.AttributeKindID(attrName)
					count := 0
					for i := 1; i <= callback.GlobalValueType().ParamTypesCount(); i++ {
						if !callback.GetEnumAttributeAtIndex(i, kind).IsNil() {
							count++
						}
					}
					if count != 1 {
						t.Fatalf("C ABI lowering retained %d %s attributes, want 1:\n%s", count, attrName, callback.String())
					}
					if err := llvm.VerifyModule(mod, llvm.ReturnStatusAction); err != nil {
						t.Fatalf("C ABI closure-env module is invalid: %v\n%s", err, mod.String())
					}
				})
			}
		})
	}
}

func TestSetSkipFuncsAndShouldSkipCall(t *testing.T) {
	tr := &Transformer{}
	tr.SetSkipFuncs([]string{" foo ", "", "bar"})

	if !tr.shouldSkipFunc("foo") {
		t.Fatalf("shouldSkipFunc(foo) = false, want true")
	}
	if !tr.shouldSkipFunc("bar") {
		t.Fatalf("shouldSkipFunc(bar) = false, want true")
	}
	if tr.shouldSkipFunc("") {
		t.Fatalf("shouldSkipFunc(\"\") = true, want false")
	}
	if tr.shouldSkipFunc("baz") {
		t.Fatalf("shouldSkipFunc(baz) = true, want false")
	}

	ctx := llvm.NewContext()
	mod := ctx.NewModule("m")
	fty := llvm.FunctionType(ctx.VoidType(), nil, false)

	callee := llvm.AddFunction(mod, "foo", fty)
	caller := llvm.AddFunction(mod, "caller", fty)
	b := ctx.NewBuilder()
	entry := ctx.AddBasicBlock(caller, "entry")
	b.SetInsertPointAtEnd(entry)
	directCall := llvm.CreateCall(b, fty, callee, nil)
	b.CreateRetVoid()
	if !tr.shouldSkipCall(directCall) {
		t.Fatalf("shouldSkipCall(direct call to foo) = false, want true")
	}

	ptrTy := llvm.PointerType(fty, 0)
	caller2Ty := llvm.FunctionType(ctx.VoidType(), []llvm.Type{ptrTy}, false)
	caller2 := llvm.AddFunction(mod, "caller2", caller2Ty)
	b2 := ctx.NewBuilder()
	entry2 := ctx.AddBasicBlock(caller2, "entry")
	b2.SetInsertPointAtEnd(entry2)
	indirectCall := b2.CreateCall(fty, caller2.Param(0), nil, "")
	b2.CreateRetVoid()
	if tr.shouldSkipCall(indirectCall) {
		t.Fatalf("shouldSkipCall(indirect call) = true, want false")
	}
}

func TestRuntimeHeaderWrapAndTypeInfo(t *testing.T) {
	llvm.InitializeAllTargets()
	llvm.InitializeAllTargetMCs()
	llvm.InitializeAllTargetInfos()

	prog := llssa.NewProgram(nil)
	defer prog.Dispose()
	tr := NewTransformer(prog, "", "", false)

	ctx := llvm.NewContext()
	ptr := llvm.PointerType(ctx.Int8Type(), 0)
	sliceTy := ctx.StructCreateNamed("github.com/xgo-dev/llgo/runtime/internal/runtime.Slice")
	sliceTy.StructSetBody([]llvm.Type{ptr, ctx.Int64Type(), ctx.Int64Type()}, false)

	if !tr.IsWrapType(ctx, llvm.FunctionType(ctx.VoidType(), nil, false), sliceTy, 1) {
		t.Fatalf("IsWrapType should be true for runtime Slice header")
	}
	info := tr.GetTypeInfo(ctx, llvm.FunctionType(ctx.VoidType(), nil, false), sliceTy, 1)
	if info.Kind == AttrNone {
		t.Fatalf("GetTypeInfo should not keep AttrNone for runtime Slice")
	}
	if info.Size == 0 || info.Align == 0 {
		t.Fatalf("GetTypeInfo size/align should be non-zero, got size=%d align=%d", info.Size, info.Align)
	}
}

func TestReflectMethodByNameNameArgAttributeRemapped(t *testing.T) {
	llvm.InitializeAllTargets()
	llvm.InitializeAllTargetMCs()
	llvm.InitializeAllTargetInfos()

	const testIR = `
%String = type { ptr, i64 }
%Value = type { ptr, ptr, i64 }

declare void @callee(%Value, %String)

define void @caller(%Value %v, %String %name) {
entry:
  call void @callee(%Value %v, %String "llgo.reflect.methodbyname.name"="1" %name) #0
  ret void
}

attributes #0 = { "llgo.reflect.methodbyname"="value" }
`
	ctx := llvm.NewContext()
	defer ctx.Dispose()

	tmpfile := filepath.Join(t.TempDir(), "reflect_methodbyname_attr.ll")
	if err := os.WriteFile(tmpfile, []byte(testIR), 0644); err != nil {
		t.Fatalf("Failed to write test IR: %v", err)
	}
	buf, err := llvm.NewMemoryBufferFromFile(tmpfile)
	if err != nil {
		t.Fatalf("Failed to read test IR: %v", err)
	}
	mod, err := ctx.ParseIR(buf)
	if err != nil {
		t.Fatalf("Failed to parse test IR: %v", err)
	}
	defer mod.Dispose()

	prog := llssa.NewProgram(nil)
	defer prog.Dispose()
	tr := NewTransformer(prog, "amd64-unknown-linux-gnu", "", true)
	tr.TransformModule("test", mod)

	caller := mod.NamedFunction("caller")
	if caller.IsNil() {
		t.Fatal("caller function not found")
	}
	ir := caller.String()
	if !strings.Contains(mod.String(), `"llgo.reflect.methodbyname"="value"`) {
		t.Fatalf("reflect MethodByName call marker was not preserved:\n%s", mod.String())
	}
	if !strings.Contains(ir, `ptr "llgo.reflect.methodbyname.name"="1"`) {
		t.Fatalf("reflect MethodByName name marker was not remapped to string data pointer:\n%s", ir)
	}
	if strings.Contains(ir, `i64 "llgo.reflect.methodbyname.name"="1"`) {
		t.Fatalf("reflect MethodByName name marker should not be remapped to string length:\n%s", ir)
	}
}

func TestPreloweredSRetAttributePreserved(t *testing.T) {
	llvm.InitializeAllTargets()
	llvm.InitializeAllTargetMCs()
	llvm.InitializeAllTargetInfos()

	const testIR = `
%Large = type [65537 x i8]
%Param = type { i64, i64, i64 }

define void @callee(ptr sret(%Large) %result, %Param %param) {
entry:
  ret void
}

define void @caller(ptr %result, %Param %param) {
entry:
  call void @callee(ptr sret(%Large) %result, %Param %param)
  ret void
}
`
	ctx := llvm.NewContext()
	defer ctx.Dispose()
	path := filepath.Join(t.TempDir(), "prelowered_sret.ll")
	if err := os.WriteFile(path, []byte(testIR), 0o644); err != nil {
		t.Fatal(err)
	}
	buf, err := llvm.NewMemoryBufferFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	mod, err := ctx.ParseIR(buf)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Dispose()

	prog := llssa.NewProgram(nil)
	defer prog.Dispose()
	tr := NewTransformer(prog, "arm64-apple-darwin", "", true)
	tr.TransformModule("test", mod)

	callee := mod.NamedFunction("callee").String()
	if !strings.Contains(callee, "define void @callee(ptr sret([65537 x i8])") {
		t.Fatalf("pre-lowered function lost its sret attribute:\n%s", callee)
	}
	caller := mod.NamedFunction("caller").String()
	if !strings.Contains(caller, "call void @callee(ptr sret([65537 x i8])") {
		t.Fatalf("pre-lowered call lost its sret attribute:\n%s", caller)
	}
	if err := llvm.VerifyModule(mod, llvm.ReturnStatusAction); err != nil {
		t.Fatalf("transformed module is invalid: %v\n%s", err, mod.String())
	}
}

func TestParamHomeReusePreservesObjectIdentity(t *testing.T) {
	llvm.InitializeAllTargets()
	llvm.InitializeAllTargetMCs()
	llvm.InitializeAllTargetInfos()

	const testIR = `
%Aggregate = type { i64, i64, i64 }

declare void @capture(ptr)
declare void @llvm.memset.p0.i64(ptr nocapture writeonly, i8, i64, i1 immarg)

define i64 @copy_then_mutate(%Aggregate %value) {
entry:
  %mutable = alloca %Aggregate, align 8
  %original = alloca %Aggregate, align 8
  store %Aggregate %value, ptr %mutable, align 8
  %mutable.field = getelementptr inbounds %Aggregate, ptr %mutable, i32 0, i32 1
  store i64 99, ptr %mutable.field, align 8
  store %Aggregate %value, ptr %original, align 8
  %original.field = getelementptr inbounds %Aggregate, ptr %original, i32 0, i32 1
  %result = load i64, ptr %original.field, align 8
  ret i64 %result
}

define i64 @rematerialize_one_copy(%Aggregate %value) {
entry:
  %copy = alloca %Aggregate, align 8
  store %Aggregate %value, ptr %copy, align 8
  store %Aggregate %value, ptr %copy, align 8
  %copy.field = getelementptr inbounds %Aggregate, ptr %copy, i32 0, i32 1
  %result = load i64, ptr %copy.field, align 8
  ret i64 %result
}

define i64 @reuse_after_memset(%Aggregate %value) {
entry:
  %copy = alloca %Aggregate, align 8
  call void @llvm.memset.p0.i64(ptr %copy, i8 0, i64 24, i1 false)
  store %Aggregate %value, ptr %copy, align 8
  %copy.field = getelementptr inbounds %Aggregate, ptr %copy, i32 0, i32 1
  %result = load i64, ptr %copy.field, align 8
  ret i64 %result
}

define i64 @reuse_natural_alignment(%Aggregate %value) {
entry:
  %copy = alloca %Aggregate
  store %Aggregate %value, ptr %copy
  %copy.field = getelementptr inbounds %Aggregate, ptr %copy, i32 0, i32 1
  %result = load i64, ptr %copy.field
  ret i64 %result
}

define i64 @reject_preinit_escape(%Aggregate %value) {
entry:
  %copy = alloca %Aggregate, align 8
  call void @capture(ptr %copy)
  store %Aggregate %value, ptr %copy, align 8
  %copy.field = getelementptr inbounds %Aggregate, ptr %copy, i32 0, i32 1
  %result = load i64, ptr %copy.field, align 8
  ret i64 %result
}

define i64 @reject_conditional_init(%Aggregate %value, i1 %cond) {
entry:
  %copy = alloca %Aggregate, align 8
  br i1 %cond, label %left, label %right
left:
  store %Aggregate %value, ptr %copy, align 8
  br label %done
right:
  store %Aggregate %value, ptr %copy, align 8
  br label %done
done:
  %copy.field = getelementptr inbounds %Aggregate, ptr %copy, i32 0, i32 1
  %result = load i64, ptr %copy.field, align 8
  ret i64 %result
}

define i64 @reject_stronger_alignment(%Aggregate %value) {
entry:
  %copy = alloca %Aggregate, align 32
  store %Aggregate %value, ptr %copy, align 32
  %copy.field = getelementptr inbounds %Aggregate, ptr %copy, i32 0, i32 1
  %result = load i64, ptr %copy.field, align 8
  ret i64 %result
}

define i64 @reject_wrong_allocated_type(%Aggregate %value) {
entry:
  %copy = alloca i8, align 8
  store %Aggregate %value, ptr %copy, align 8
  %copy.field = getelementptr inbounds %Aggregate, ptr %copy, i32 0, i32 1
  %result = load i64, ptr %copy.field, align 8
  ret i64 %result
}

define i64 @reject_multiple_elements(%Aggregate %value) {
entry:
  %copy = alloca %Aggregate, i64 2, align 8
  store %Aggregate %value, ptr %copy, align 8
  %copy.field = getelementptr inbounds %Aggregate, ptr %copy, i32 0, i32 1
  %result = load i64, ptr %copy.field, align 8
  ret i64 %result
}

define i64 @reject_dynamic_elements(%Aggregate %value, i64 %count) {
entry:
  %copy = alloca %Aggregate, i64 %count, align 8
  store %Aggregate %value, ptr %copy, align 8
  %copy.field = getelementptr inbounds %Aggregate, ptr %copy, i32 0, i32 1
  %result = load i64, ptr %copy.field, align 8
  ret i64 %result
}
`

	for _, test := range []struct {
		name   string
		goos   string
		goarch string
		triple string
	}{
		{name: "amd64", goos: "linux", goarch: "amd64", triple: "x86_64-unknown-linux-gnu"},
		{name: "arm64", goos: "darwin", goarch: "arm64", triple: "arm64-apple-darwin"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := llvm.NewContext()
			defer ctx.Dispose()
			path := filepath.Join(t.TempDir(), "param_copies.ll")
			if err := os.WriteFile(path, []byte(testIR), 0o644); err != nil {
				t.Fatal(err)
			}
			buf, err := llvm.NewMemoryBufferFromFile(path)
			if err != nil {
				t.Fatal(err)
			}
			mod, err := ctx.ParseIR(buf)
			if err != nil {
				t.Fatal(err)
			}
			defer mod.Dispose()

			prog := llssa.NewProgram(&llssa.Target{GOOS: test.goos, GOARCH: test.goarch})
			defer prog.Dispose()
			NewTransformer(prog, test.triple, "", true).TransformModule("test", mod)

			got := mod.NamedFunction("copy_then_mutate").String()
			if strings.Contains(got, "getelementptr inbounds %Aggregate, ptr %mutable") {
				t.Fatalf("C ABI lowering did not reuse the first proven parameter home:\n%s", got)
			}
			if !strings.Contains(got, "getelementptr inbounds %Aggregate, ptr %original") {
				t.Fatalf("C ABI lowering aliased the independent parameter copy:\n%s", got)
			}
			singleCopy := mod.NamedFunction("rematerialize_one_copy").String()
			if strings.Contains(singleCopy, "getelementptr inbounds %Aggregate, ptr %copy") {
				t.Fatalf("C ABI lowering did not reuse the indirect parameter for one alloca:\n%s", singleCopy)
			}
			memsetCopy := mod.NamedFunction("reuse_after_memset").String()
			if !strings.Contains(memsetCopy, "call void @llvm.memset.p0.i64(ptr %copy") {
				t.Fatalf("C ABI lowering moved the pre-initialization memset to the parameter home:\n%s", memsetCopy)
			}
			if strings.Contains(memsetCopy, "getelementptr inbounds %Aggregate, ptr %copy") {
				t.Fatalf("C ABI lowering did not reuse storage after a direct memset:\n%s", memsetCopy)
			}
			naturalAlignment := mod.NamedFunction("reuse_natural_alignment").String()
			if strings.Contains(naturalAlignment, "getelementptr inbounds %Aggregate, ptr %copy") {
				t.Fatalf("C ABI lowering did not use the alloca type's natural alignment:\n%s", naturalAlignment)
			}
			for _, name := range []string{
				"reject_preinit_escape",
				"reject_conditional_init",
				"reject_stronger_alignment",
				"reject_wrong_allocated_type",
				"reject_multiple_elements",
				"reject_dynamic_elements",
			} {
				got := mod.NamedFunction(name).String()
				if !strings.Contains(got, "getelementptr inbounds %Aggregate, ptr %copy") {
					t.Fatalf("C ABI lowering reused an unproven parameter home in %s:\n%s", name, got)
				}
			}
			if err := llvm.VerifyModule(mod, llvm.ReturnStatusAction); err != nil {
				t.Fatalf("lowered parameter-copy module is invalid: %v\n%s", err, mod.String())
			}
		})
	}
}

func TestCanReuseParamHomeRequiresEntryBlockOrder(t *testing.T) {
	const testIR = `
%Aggregate = type { i64, i64, i64 }

define void @non_entry_alloca(%Aggregate %value) {
entry:
  br label %body
body:
  %copy = alloca %Aggregate, align 8
  store %Aggregate %value, ptr %copy, align 8
  ret void
}

define void @init_before_alloca(%Aggregate %value) {
entry:
  %source = alloca %Aggregate, align 8
  store %Aggregate %value, ptr %source, align 8
  %copy = alloca %Aggregate, align 8
  ret void
}
`

	ctx := llvm.NewContext()
	defer ctx.Dispose()
	path := filepath.Join(t.TempDir(), "param_home_order.ll")
	if err := os.WriteFile(path, []byte(testIR), 0o644); err != nil {
		t.Fatal(err)
	}
	buf, err := llvm.NewMemoryBufferFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	mod, err := ctx.ParseIR(buf)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Dispose()

	nonEntry := mod.NamedFunction("non_entry_alloca")
	entry := nonEntry.EntryBasicBlock()
	body := llvm.NextBasicBlock(entry)
	alloc := body.FirstInstruction()
	initStore := llvm.NextInstruction(alloc)
	if canReuseParamHome(alloc, initStore, entry, nonEntry.Param(0).Type(), 8, 8) {
		t.Fatal("parameter home outside the entry block was accepted")
	}

	earlierInit := mod.NamedFunction("init_before_alloca")
	entry = earlierInit.EntryBasicBlock()
	initStore = llvm.NextInstruction(entry.FirstInstruction())
	alloc = llvm.NextInstruction(initStore)
	alloc.SetAlignment(0)
	if alloc.Alignment() != 0 {
		t.Fatalf("failed to clear candidate alloca alignment: got %d", alloc.Alignment())
	}
	if canReuseParamHome(alloc, initStore, entry, earlierInit.Param(0).Type(), 8, 8) {
		t.Fatal("parameter initialization before its candidate alloca was accepted")
	}
}

func TestWidthReturnUsesEvaluatedValue(t *testing.T) {
	llvm.InitializeAllTargets()
	llvm.InitializeAllTargetMCs()
	llvm.InitializeAllTargetInfos()

	const testIR = `
%Small = type { i32, i32 }
%Pair = type { i64, i64 }

define %Small @return_loaded_snapshot(ptr %src) {
entry:
  %snapshot = load %Small, ptr %src, align 4
  store %Small { i32 9, i32 10 }, ptr %src, align 4
  ret %Small %snapshot
}

define i32 @width_param_copies(%Small %value) {
entry:
  %first = alloca %Small, align 4
  %second = alloca %Small, align 4
  store %Small %value, ptr %first, align 4
  %first.field = getelementptr inbounds %Small, ptr %first, i32 0, i32 0
  store i32 99, ptr %first.field, align 4
  store %Small %value, ptr %second, align 4
  %second.field = getelementptr inbounds %Small, ptr %second, i32 0, i32 0
  %result = load i32, ptr %second.field, align 4
  ret i32 %result
}

define i64 @two_width_param_copies(%Pair %value) {
entry:
  %first = alloca %Pair, align 8
  %second = alloca %Pair, align 8
  store %Pair %value, ptr %first, align 8
  %first.field = getelementptr inbounds %Pair, ptr %first, i32 0, i32 0
  store i64 99, ptr %first.field, align 8
  store %Pair %value, ptr %second, align 8
  %second.field = getelementptr inbounds %Pair, ptr %second, i32 0, i32 0
  %result = load i64, ptr %second.field, align 8
  ret i64 %result
}
`

	ctx := llvm.NewContext()
	defer ctx.Dispose()
	path := filepath.Join(t.TempDir(), "width_return.ll")
	if err := os.WriteFile(path, []byte(testIR), 0o644); err != nil {
		t.Fatal(err)
	}
	buf, err := llvm.NewMemoryBufferFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	mod, err := ctx.ParseIR(buf)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Dispose()

	prog := llssa.NewProgram(&llssa.Target{GOOS: "linux", GOARCH: "amd64"})
	defer prog.Dispose()
	NewTransformer(prog, "x86_64-unknown-linux-gnu", "", true).TransformModule("test", mod)

	got := mod.NamedFunction("return_loaded_snapshot").String()
	if !strings.Contains(got, "ret i64") {
		t.Fatalf("small aggregate return was not width-lowered:\n%s", got)
	}
	if strings.Contains(got, "load i64, ptr %src") {
		t.Fatalf("C ABI lowering re-read a return load's modified source:\n%s", got)
	}
	for _, test := range []struct {
		name string
		typ  string
	}{
		{name: "width_param_copies", typ: "%Small"},
		{name: "two_width_param_copies", typ: "%Pair"},
	} {
		got := mod.NamedFunction(test.name).String()
		if strings.Contains(got, "getelementptr inbounds "+test.typ+", ptr %first") {
			t.Fatalf("C ABI lowering did not reuse the first %s parameter home:\n%s", test.name, got)
		}
		if !strings.Contains(got, "getelementptr inbounds "+test.typ+", ptr %second") {
			t.Fatalf("C ABI lowering aliased an independent %s parameter copy:\n%s", test.name, got)
		}
	}
	if err := llvm.VerifyModule(mod, llvm.ReturnStatusAction); err != nil {
		t.Fatalf("lowered width-return module is invalid: %v\n%s", err, mod.String())
	}
}
