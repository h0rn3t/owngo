package ssa

// IsConstant reports whether v is an LLVM constant, such as a global's address.
func (v Expr) IsConstant() bool {
	return !v.impl.IsNil() && v.impl.IsConstant()
}
