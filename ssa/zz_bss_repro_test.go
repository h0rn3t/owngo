package ssa

import (
	"strings"
	"testing"

	"github.com/xgo-dev/llvm"
)

// Scratch repro for: ld.lld BSS section '.bss$os.errWriteAtInAppendMode.0'
// cannot have non-zero bytes (windows mingw -lto=full, LLVM 23).
func TestScratchCOFFBSSSections(t *testing.T) {
	Initialize(InitAllTargets | InitAllTargetInfos | InitAllTargetMCs | InitAllAsmPrinters)
	target, err := llvm.GetTargetFromTriple("x86_64-pc-windows-msvc")
	if err != nil {
		t.Fatal(err)
	}
	machine := target.CreateTargetMachineWithOptions(
		"x86_64-pc-windows-msvc", "generic", "",
		llvm.CodeGenLevelDefault, llvm.RelocPIC, llvm.CodeModelDefault,
		llvm.TargetMachineOptions{FunctionSections: true, DataSections: true, UniqueSectionNames: true},
	)
	defer machine.Dispose()

	build := func(initNull bool, linkage llvm.Linkage, comdat bool) llvm.Module {
		lc := llvm.NewContext()
		mod := lc.NewModule("repro")
		i8ptr := llvm.PointerType(lc.Int8Type(), 0)
		ctrTy := llvm.StructType([]llvm.Type{i8ptr, lc.Int64Type()}, false)
		ctr := llvm.AddGlobal(mod, ctrTy, "os.errCtr")
		ctr.SetInitializer(llvm.ConstNull(ctrTy))
		ctr.SetLinkage(llvm.PrivateLinkage)
		g := llvm.AddGlobal(mod, i8ptr, "os.errWriteAtInAppendMode")
		if initNull {
			g.SetInitializer(llvm.ConstNull(i8ptr))
		} else {
			g.SetInitializer(llvm.ConstBitCast(ctr, i8ptr))
		}
		g.SetLinkage(linkage)
		g.SetAlignment(8)
		if comdat {
			c := mod.Comdat("os.errWriteAtInAppendMode")
			c.SetSelectionKind(llvm.AnyComdatSelectionKind)
			g.SetComdat(c)
		}
		if initNull {
			// Mimic llgo package init: store the constructed value.
			fnTy := llvm.FunctionType(lc.VoidType(), nil, false)
			init := llvm.AddFunction(mod, "p.init", fnTy)
			init.SetLinkage(llvm.InternalLinkage)
			b := lc.NewBuilder()
			defer b.Dispose()
			bb := lc.AddBasicBlock(init, "entry")
			b.SetInsertPointAtEnd(bb)
			b.CreateStore(llvm.ConstBitCast(ctr, i8ptr), g)
			b.CreateRetVoid()
		}
		return mod
	}

	for _, tc := range []struct {
		name       string
		initNull   bool
		linkage    llvm.Linkage
		comdat     bool
		ltoPreLink bool
	}{
		{"external-initialized", false, llvm.ExternalLinkage, false, false},
		{"external-null", true, llvm.ExternalLinkage, false, false},
		{"external-null-lto-pre-link", true, llvm.ExternalLinkage, false, true},
		{"weakodr-initialized-comdat", false, llvm.WeakODRLinkage, true, false},
		{"weakodr-initialized-comdat-lto-pre-link", false, llvm.WeakODRLinkage, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mod := build(tc.initNull, tc.linkage, tc.comdat)
			defer mod.Dispose()
			opts := llvm.NewPassBuilderOptions()
			defer opts.Dispose()
			pipeline := "default<O2>"
			if tc.ltoPreLink {
				pipeline = "lto-pre-link<O2>"
			}
			if err := mod.RunPasses(pipeline, machine, opts); err != nil {
				t.Fatal(err)
			}
			asm, err := machine.EmitToMemoryBuffer(mod, llvm.AssemblyFile)
			if err != nil {
				t.Fatal(err)
			}
			defer asm.Dispose()
			for _, line := range strings.Split(string(asm.Bytes()), "\n") {
				if strings.Contains(line, "section") || strings.HasPrefix(line, "@") {
					t.Log("ASM:", line)
				}
			}
		})
	}
}
