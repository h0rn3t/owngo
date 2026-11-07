//go:build owngo && !baremetal

package runtime

import (
	"unsafe"

	rt "github.com/xgo-dev/llgo/runtime/internal/runtime"
)

// freeSleepChan звільняє канал time.Sleep після прийому: таймер одноразовий,
// і інших посилань на канал немає.
func freeSleepChan(done chan struct{}) {
	rt.FreeChan(*(**rt.Chan)(unsafe.Pointer(&done)))
}
