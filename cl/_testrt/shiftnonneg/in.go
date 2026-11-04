// LITTEST
package main

// A shift by a signed count needs the negative-shift check only when the
// count may be negative. Counts built from masks, zero extension, unsigned
// division and bounded arithmetic are provably non-negative and shift without
// AssertNegativeShift; counts that may be negative or may overflow keep it.

func shlMasked(x uint64, k int) uint64 { return x << (k & 63) }

func shrByte(x uint64, b uint8) uint64 { return x >> int(b) }

func shlNibble(x, perm uint64) uint64 { return x << (4*int(perm&0xF) + 1) }

func shrHalf(x, u uint64) uint64 { return x >> int(u>>1) }

func shlRem(x uint64, u uint) uint64 { return x << int(u%64) }

func shlSigned(x uint64, k int) uint64 { return x << k }

func shlNegMask(x uint64, k int) uint64 { return x << (k & -2) }

// The sum of two counts up to 2⁶³-1 wraps to a negative int.
func shlWrap(x, u uint64) uint64 { return x << (int(u>>1) + int(u>>1)) }

func shift(f func()) {
	defer func() { println(recover().(error).Error()) }()
	f()
}

func main() {
	println(shlMasked(1, 67), shrByte(1<<40, 40), shlNibble(1, 0xF3), shrHalf(1<<62, 124), shlRem(1, 130))
	println(shlSigned(1, 3), shlNegMask(1, 5), shlWrap(1, 4))
	shift(func() { shlSigned(1, -1) })
	shift(func() { shlNegMask(1, -1) })
	shift(func() { shlWrap(1, 1<<64-1) })
}

// Functions appear in the IR in name order.

// CHECK-LABEL: define i64 @main.shlMasked(
// CHECK-NOT: AssertNegativeShift
// CHECK: ret i64

// CHECK-LABEL: define i64 @main.shlNegMask(
// CHECK: call void @"{{.*}}AssertNegativeShift"

// CHECK-LABEL: define i64 @main.shlNibble(
// CHECK-NOT: AssertNegativeShift
// CHECK: ret i64

// CHECK-LABEL: define i64 @main.shlRem(
// CHECK-NOT: AssertNegativeShift
// CHECK: ret i64

// CHECK-LABEL: define i64 @main.shlSigned(
// CHECK: call void @"{{.*}}AssertNegativeShift"

// CHECK-LABEL: define i64 @main.shlWrap(
// CHECK: call void @"{{.*}}AssertNegativeShift"

// CHECK-LABEL: define i64 @main.shrByte(
// CHECK-NOT: AssertNegativeShift
// CHECK: ret i64

// CHECK-LABEL: define i64 @main.shrHalf(
// CHECK-NOT: AssertNegativeShift
// CHECK: ret i64
