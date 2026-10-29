# OwnGo patches to github.com/xgo-dev/llvm

Copy of `github.com/xgo-dev/llvm` v0.10.1, wired in through a `replace`
directive in the root `go.mod`. Upstream supports LLVM up to 22; this copy
targets LLVM 23 only.

- `llvm_config_llvm23.go` (was `llvm_config_llvm22.go`): cgo flags point at
  LLVM 23 and it is the default when no `llvmNN` tag is set.
- `ir.go`: LLVM 23 removed the `LLVMBr` opcode. `Br` is now `LLVMUncondBr`;
  the new `CondBr` is `LLVMCondBr`.
- `ir.go` `ConstInt`: LLVM 23's `LLVMConstInt` no longer truncates the value
  to the type width; leftover high bits made equal values distinct constants
  and miscompiled comparisons at -O1 and above. `ConstInt` masks them.
- `IRBindings.cpp`: `matchIntrinsicSignature` was removed; use
  `Intrinsic::isSignatureValid`.
- `backports.cpp`: LLVM 23 keys module summaries by GUID metadata from
  `AssignGUIDPass`; the LTO bitcode writers run it first, since llgo adds
  globals after the optimization pipeline.

Drop this copy once upstream ships LLVM 23 support.
