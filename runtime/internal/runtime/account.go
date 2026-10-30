//go:build !owngo

package runtime

// ReportExit — порожній гачок виходу, коли тег owngo не заданий.
func ReportExit() {}
