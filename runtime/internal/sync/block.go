package sync

// blockEnter і blockExit ставить планувальник M:N (тег owngomn): потік, що
// блокується засобами ОС, віддає логічний процесор іншому потоку і бере
// його знову після пробудження. Без планувальника обидва — nil.
var blockEnter, blockExit func()

// SetBlockHooks ставить хуки блокування; кличе рантайм один раз до першої
// горутини.
func SetBlockHooks(enter, exit func()) {
	blockEnter, blockExit = enter, exit
}

// BlockEnter позначає початок блокувального виклику ОС у поточному потоці.
func BlockEnter() {
	if blockEnter != nil {
		blockEnter()
	}
}

// BlockExit позначає кінець блокувального виклику, почату BlockEnter.
func BlockExit() {
	if blockExit != nil {
		blockExit()
	}
}
