//go:build go1.25
// +build go1.25

package runtime

import (
	_ "unsafe"
)

const (
	// Every hosted target uses the high-resolution nanotime trace path.
	osHasLowResClockInt = 0
)

const traceTimeDiv = (1-osHasLowResClockInt)*64 + osHasLowResClockInt*(256-224*(IsPpc64|IsPpc64le))

// traceClockUnitsPerSecond estimates the number of trace clock units per
// second that elapse.
//
//go:linkname traceClockUnitsPerSecond runtime/trace.runtime_traceClockUnitsPerSecond
func traceClockUnitsPerSecond() uint64 {
	// Our clock is nanotime, so it's just the constant time division.
	// (trace clock units / nanoseconds) * (1e9 nanoseconds / 1 second)
	return uint64(1.0 / float64(traceTimeDiv) * 1e9)
}
