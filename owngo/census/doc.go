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
// StdlibPackages ділить рядок stdlib за імпортом пакета першого кадру.
// StdlibCategories з'єднує цей рядок з місцями v1 за file:line.
// ExternalModules ділить рядок поза -dir за шляхом модуля з pkg/mod.
// ExternalCategories з'єднує цей рядок з місцями v1 за file:line.
// Файл кадру, якого вже немає на диску, лишається рядком немає джерела.
// ExternalPackageDirs перелічує каталоги пакетів цього рядка, чий файл кадру ще на диску.
package census
