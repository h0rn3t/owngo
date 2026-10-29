// Package census класифікує місця алокації Go-програми для фази вимірів OwnGo.
//
// Analyzer знаходить місця після buildssa і звітує категорію v0
// (stack, static, rc-local, rc-shared, cyclic, pinned, implicit).
package census
