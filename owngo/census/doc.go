// Package census класифікує місця алокації Go-програми для фази вимірів OwnGo.
//
// Analyzer знаходить місця після buildssa і звітує категорію v0
// (stack, static, rc-local, rc-shared, cyclic, pinned, implicit).
// StackGaps порівнює місця поза stack із діагностиками escape analysis gc.
// ParseAllocProfile і Weigh з'єднують місця з профілем pprof
// (alloc_objects, alloc_space) за file:line.
package census
