package cl

import (
	"strings"
	"testing"

	"github.com/xgo-dev/llvm"
)

const blockingCallSrc = `
package foo

import "unsafe"

//go:linkname sleep C.sleep
func sleep(seconds uint32) uint32

//go:linkname memcpy C.memcpy
func memcpy(dst, src unsafe.Pointer, n uintptr) unsafe.Pointer

//go:linkname malloc C.malloc
func malloc(n uintptr) unsafe.Pointer

var _cgo_usleep unsafe.Pointer
func _cgo_runtime_cgocall(fn unsafe.Pointer, arg unsafe.Pointer) int

func _Cfunc_usleep(us uint32) int32 {
	_cgo_runtime_cgocall(_cgo_usleep, nil)
	return 0
}

func blocking() uint32 {
	return sleep(1)
}

func nonBlocking(dst, src unsafe.Pointer) unsafe.Pointer {
	p := malloc(8)
	memcpy(dst, src, 8)
	return p
}

//export goCallback
func goCallback(n int32) int32 {
	return n + 1
}
`

func compileBlockingCallMarkers(t *testing.T, enable bool) llvm.Module {
	t.Helper()
	ssaPkg, _, files := buildGoSSAPkg(t, blockingCallSrc)
	prog := newLLSSAProg(t)
	prog.EnableBlockingCallMarkers(enable)
	pkg, err := NewPackage(prog, ssaPkg, files)
	if err != nil {
		t.Fatal(err)
	}
	return pkg.Module()
}

// Під owngomn виклик функції C, що може блокуватися, і виклик cgo стоять між
// EnterSyscall і ExitSyscall, а memcpy і malloc — ні; вхід //export
// обгорнуто в CallbackEnter/CallbackExit.
func TestBlockingCallMarkers(t *testing.T) {
	m := compileBlockingCallMarkers(t, true)
	for _, tc := range []struct{ fn, call string }{
		{"foo.blocking", "@sleep("},
		{"foo._Cfunc_usleep", "call i32 %"},
	} {
		ir := mustNamedFunction(t, m, tc.fn).String()
		enter := strings.Index(ir, "runtime.EnterSyscall")
		call := strings.Index(ir, tc.call)
		exit := strings.Index(ir, "runtime.ExitSyscall")
		if enter < 0 || call < 0 || exit < 0 || !(enter < call && call < exit) {
			t.Errorf("%s: C call %q is not between EnterSyscall and ExitSyscall:\n%s", tc.fn, tc.call, ir)
		}
	}
	if ir := mustNamedFunction(t, m, "foo.nonBlocking").String(); strings.Contains(ir, "Syscall") {
		t.Errorf("foo.nonBlocking: memcpy or malloc is marked:\n%s", ir)
	}
	ir := mustNamedFunction(t, m, "goCallback").String()
	enter := strings.Index(ir, "runtime.CallbackEnter")
	exit := strings.Index(ir, "runtime.CallbackExit")
	ret := strings.LastIndex(ir, "ret i32")
	if enter < 0 || exit < 0 || !(enter < exit && exit < ret) {
		t.Errorf("goCallback: exported entry is not wrapped in CallbackEnter/CallbackExit:\n%s", ir)
	}
}

// Без owngomn маркерів немає ніде.
func TestBlockingCallMarkersDisabled(t *testing.T) {
	m := compileBlockingCallMarkers(t, false)
	for _, fn := range []string{"foo.blocking", "foo._Cfunc_usleep", "foo.nonBlocking", "goCallback"} {
		ir := mustNamedFunction(t, m, fn).String()
		if strings.Contains(ir, "Syscall") || strings.Contains(ir, "runtime.Callback") {
			t.Errorf("%s: markers without owngomn:\n%s", fn, ir)
		}
	}
}
