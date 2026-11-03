//go:build windows

package win

import (
	"syscall"
	"unsafe"
)

// Minimal COM plumbing: enough to drive Media Foundation and WASAPI without
// CGO. Interfaces are raw pointers, methods are called through the vtable.

const (
	S_OK                     = 0
	S_FALSE                  = 1
	CLASS_E_NOAGGR           = -2147221184
	E_INVALIDARG             = -2147024809
	CLSCTX_ALL               = 0x17
	COINIT_APARTMENTTHREADED = 0x2
	COINIT_MULTITHREADED     = 0x0
)

var (
	pCoInitializeEx   = dllOle32.NewProc("CoInitializeEx")
	pCoUninitialize   = dllOle32.NewProc("CoUninitialize")
	pCoCreateInstance = dllOle32.NewProc("CoCreateInstance")
)

// comComInit initialises COM on the calling thread (MTA). Safe to call twice.
func ComInit() bool {
	r, _, _ := pCoInitializeEx.Call(0, COINIT_MULTITHREADED)
	hr := int32(r)
	return hr == S_OK || hr == S_FALSE || hr == -2147417850 // RPC_E_CHANGED_MODE
}

func ComUninit() { pCoUninitialize.Call() }

// vtbl returns the function pointer of method #index.
func vtbl(p unsafe.Pointer, index int) uintptr {
	v := *(*unsafe.Pointer)(p)
	return *(*uintptr)(unsafe.Add(v, uintptr(index)*unsafe.Sizeof(uintptr(0))))
}

// comCall invokes method #index on a COM interface and returns the HRESULT.
func comCall(p unsafe.Pointer, index int, args ...uintptr) int32 {
	if p == nil {
		return E_INVALIDARG
	}
	fn := vtbl(p, index)
	all := make([]uintptr, 0, len(args)+1)
	all = append(all, uintptr(p))
	all = append(all, args...)
	r, _, _ := syscall.SyscallN(fn, all...)
	return int32(r)
}

// comCallR is comCall that also returns the raw return value (for methods whose
// result arrives in EAX rather than through an out parameter).
func comCallR(p unsafe.Pointer, index int, args ...uintptr) (int32, uintptr) {
	if p == nil {
		return E_INVALIDARG, 0
	}
	fn := vtbl(p, index)
	all := make([]uintptr, 0, len(args)+1)
	all = append(all, uintptr(p))
	all = append(all, args...)
	r, _, _ := syscall.SyscallN(fn, all...)
	return int32(r), r
}

func ComRelease(p unsafe.Pointer) {
	if p != nil {
		comCall(p, 2)
	}
}

func (g GUID) Ptr() uintptr { return uintptr(unsafe.Pointer(&g)) }

// ComCreateInstance instantiates a class into the given interface.
func ComCreateInstance(clsid, iid GUID) (unsafe.Pointer, int32) {
	var out unsafe.Pointer
	hr, _, _ := pCoCreateInstance.Call(
		clsid.Ptr(), 0, CLSCTX_ALL, iid.Ptr(), uintptr(unsafe.Pointer(&out)))
	return out, int32(hr)
}

func HRFailed(hr int32) bool { return hr < 0 }
