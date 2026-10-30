// Package census класифікує місця алокації Go-програми для фази вимірів OwnGo.
//
// Analyzer знаходить місця після buildssa і звітує категорію v0
// (stack, static, rc-local, rc-shared, cyclic, pinned, implicit).
// Прапорець -v1 замінює причину call на позичання, переміщення або спільне.
// StaticUses віддає ті самі правила v1 компілятору: місця static і інструкції, які їх читають.
// StackGaps порівнює місця поза stack із діагностиками escape analysis gc.
// ParseAllocProfile і Weigh з'єднують місця з профілем pprof
// (alloc_objects, alloc_space) за file:line.
// Outside розкладає рядок поза census на стандартну бібліотеку,
// модулі поза каталогом, тести, runtime і файли модуля без місця.
package census
