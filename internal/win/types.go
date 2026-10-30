//go:build windows

package win

import (
	"syscall"
	"unsafe"
)

type HWND uintptr
type HDC uintptr
type HGDIOBJ uintptr
type HBITMAP uintptr
type HICON uintptr
type HMENU uintptr
type HBRUSH uintptr
type HPEN uintptr
type HFONT uintptr
type HINSTANCE uintptr
type HANDLE uintptr
type HKEY uintptr
type ATOM uint16
type WPARAM uintptr
type LPARAM uintptr
type LRESULT uintptr

type POINT struct{ X, Y int32 }

type RECT struct{ Left, Top, Right, Bottom int32 }

func (r RECT) W() int32    { return r.Right - r.Left }
func (r RECT) H() int32    { return r.Bottom - r.Top }
func (r RECT) Empty() bool { return r.Right <= r.Left || r.Bottom <= r.Top }

type SIZE struct{ CX, CY int32 }

type MSG struct {
	HWnd    HWND
	Message uint32
	WParam  WPARAM
	LParam  LPARAM
	Time    uint32
	Pt      POINT
	Private uint32
}

type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     HINSTANCE
	HIcon         HICON
	HCursor       uintptr
	HbrBackground HBRUSH
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       HICON
}

type BITMAPINFOHEADER struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}

type BITMAPINFO struct {
	BmiHeader BITMAPINFOHEADER
	BmiColors [1]uint32
}

type MONITORINFO struct {
	CbSize    uint32
	RcMonitor RECT
	RcWork    RECT
	DwFlags   uint32
}

type NOTIFYICONDATAW struct {
	CbSize           uint32
	HWnd             HWND
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            HICON
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     HICON
}

type ICONINFO struct {
	FIcon    int32
	XHotspot uint32
	YHotspot uint32
	HbmMask  HBITMAP
	HbmColor HBITMAP
}

type BLENDFUNCTION struct {
	BlendOp             byte
	BlendFlags          byte
	SourceConstantAlpha byte
	AlphaFormat         byte
}

type CREATESTRUCTW struct {
	LpCreateParams uintptr
	HInstance      HINSTANCE
	HMenu          HMENU
	HWndParent     HWND
	Cy, Cx         int32
	Y, X           int32
	Style          int32
	LpszName       *uint16
	LpszClass      *uint16
	DwExStyle      uint32
}

// ---- constants ----

const (
	WM_NULL           = 0x0000
	WM_CREATE         = 0x0001
	WM_DESTROY        = 0x0002
	WM_MOVE           = 0x0003
	WM_SIZE           = 0x0005
	WM_ACTIVATE       = 0x0006
	WM_SETFOCUS       = 0x0007
	WM_KILLFOCUS      = 0x0008
	WM_PAINT          = 0x000F
	WM_CLOSE          = 0x0010
	WM_QUIT           = 0x0012
	WM_ERASEBKGND     = 0x0014
	WM_SHOWWINDOW     = 0x0018
	WM_ACTIVATEAPP    = 0x001C
	WM_SETCURSOR      = 0x0020
	WM_MOUSEACTIVATE  = 0x0021
	WM_GETMINMAXINFO  = 0x0024
	WM_WINDOWPOSCHG   = 0x0047
	WM_DISPLAYCHANGE  = 0x007E
	WM_NCCREATE       = 0x0081
	WM_NCHITTEST      = 0x0084
	WM_NCPAINT        = 0x0085
	WM_KEYDOWN        = 0x0100
	WM_KEYUP          = 0x0101
	WM_CHAR           = 0x0102
	WM_SYSKEYDOWN     = 0x0104
	WM_SYSKEYUP       = 0x0105
	WM_COMMAND        = 0x0111
	WM_TIMER          = 0x0113
	WM_MOUSEMOVE      = 0x0200
	WM_LBUTTONDOWN    = 0x0201
	WM_LBUTTONUP      = 0x0202
	WM_LBUTTONDBLCLK  = 0x0203
	WM_RBUTTONDOWN    = 0x0204
	WM_RBUTTONUP      = 0x0205
	WM_MOUSEWHEEL     = 0x020A
	WM_CAPTURECHANGED = 0x0215
	WM_CONTEXTMENU    = 0x007B
	WM_MOUSELEAVE     = 0x02A3
	WM_HOTKEY         = 0x0312
	WM_APP            = 0x8000
	WM_TRAYICON       = WM_APP + 1
	WM_CAPTURED       = WM_APP + 2
)

const (
	WS_OVERLAPPED   = 0x00000000
	WS_POPUP        = 0x80000000
	WS_CHILD        = 0x40000000
	WS_MINIMIZE     = 0x20000000
	WS_VISIBLE      = 0x10000000
	WS_DISABLED     = 0x08000000
	WS_CLIPSIBLINGS = 0x04000000
	WS_CLIPCHILDREN = 0x02000000
	WS_CAPTION      = 0x00C00000
	WS_BORDER       = 0x00800000
	WS_DLGFRAME     = 0x00400000
	WS_THICKFRAME   = 0x00040000
	WS_SYSMENU      = 0x00080000
)

const (
	WS_EX_TOPMOST     = 0x00000008
	WS_EX_TRANSPARENT = 0x00000020
	WS_EX_TOOLWINDOW  = 0x00000080
	WS_EX_LAYERED     = 0x00080000
	WS_EX_NOACTIVATE  = 0x08000000
	WS_EX_APPWINDOW   = 0x00040000
	WS_EX_COMPOSITED  = 0x02000000
	WS_EX_WINDOWEDGE  = 0x00000100
)

const (
	SW_HIDE           = 0
	SW_SHOWNORMAL     = 1
	SW_SHOWNOACTIVATE = 4
	SW_SHOW           = 5
	SW_MINIMIZE       = 6
	SW_SHOWNA         = 8
)

const HWND_TOPMOST = HWND(^uintptr(0)) // -1
const HWND_TOP = HWND(0)

const (
	SWP_NOSIZE        = 0x0001
	SWP_NOMOVE        = 0x0002
	SWP_NOZORDER      = 0x0004
	SWP_NOREDRAW      = 0x0008
	SWP_NOACTIVATE    = 0x0010
	SWP_FRAMECHANGED  = 0x0020
	SWP_SHOWWINDOW    = 0x0040
	SWP_HIDEWINDOW    = 0x0080
	SWP_NOOWNERZORDER = 0x0200
)

const (
	GWL_STYLE     = ^uintptr(15) // -16
	GWL_EXSTYLE   = ^uintptr(19) // -20
	GWLP_USERDATA = ^uintptr(20) // -21
)

const (
	SM_CXSCREEN        = 0
	SM_CYSCREEN        = 1
	SM_XVIRTUALSCREEN  = 76
	SM_YVIRTUALSCREEN  = 77
	SM_CXVIRTUALSCREEN = 78
	SM_CYVIRTUALSCREEN = 79
	SM_CMONITORS       = 80
	SM_SWAPBUTTON      = 23
	SM_REMOTESESSION   = 0x1000
	SM_CXDRAG          = 68
	SM_CYDRAG          = 69
	SM_MOUSEPRESENT    = 19
)

const (
	IDC_ARROW    = 32512
	IDC_IBEAM    = 32513
	IDC_CROSS    = 32515
	IDC_WAIT     = 32514
	IDC_SIZEALL  = 32646
	IDC_HAND     = 32649
	IDC_NO       = 32648
	IDC_SIZENWSE = 32642
	IDC_SIZENESW = 32643
	IDC_SIZEWE   = 32644
	IDC_SIZENS   = 32645
)

const (
	MOD_ALT      = 0x0001
	MOD_CONTROL  = 0x0002
	MOD_SHIFT    = 0x0004
	MOD_WIN      = 0x0008
	MOD_NOREPEAT = 0x4000
)

const (
	VK_BACK      = 0x08
	VK_TAB       = 0x09
	VK_RETURN    = 0x0D
	VK_SHIFT     = 0x10
	VK_CONTROL   = 0x11
	VK_MENU      = 0x12
	VK_ESCAPE    = 0x1B
	VK_SPACE     = 0x20
	VK_PRIOR     = 0x21
	VK_NEXT      = 0x22
	VK_END       = 0x23
	VK_HOME      = 0x24
	VK_LEFT      = 0x25
	VK_UP        = 0x26
	VK_RIGHT     = 0x27
	VK_DOWN      = 0x28
	VK_DELETE    = 0x2E
	VK_SNAPSHOT  = 0x2C
	VK_INSERT    = 0x2D
	VK_0         = 0x30
	VK_1         = 0x31
	VK_2         = 0x32
	VK_3         = 0x33
	VK_4         = 0x34
	VK_5         = 0x35
	VK_6         = 0x36
	VK_7         = 0x37
	VK_8         = 0x38
	VK_9         = 0x39
	VK_A         = 0x41
	VK_B         = 0x42
	VK_C         = 0x43
	VK_D         = 0x44
	VK_E         = 0x45
	VK_F         = 0x46
	VK_G         = 0x47
	VK_H         = 0x48
	VK_I         = 0x49
	VK_J         = 0x4A
	VK_K         = 0x4B
	VK_L         = 0x4C
	VK_M         = 0x4D
	VK_N         = 0x4E
	VK_O         = 0x4F
	VK_P         = 0x50
	VK_Q         = 0x51
	VK_R         = 0x52
	VK_S         = 0x53
	VK_T         = 0x54
	VK_U         = 0x55
	VK_V         = 0x56
	VK_W         = 0x57
	VK_X         = 0x58
	VK_Y         = 0x59
	VK_Z         = 0x5A
	VK_OEM_PLUS  = 0xBB
	VK_OEM_MINUS = 0xBD
)

const (
	SRCCOPY    = 0x00CC0020
	SRCPAINT   = 0x00EE0086
	SRCAND     = 0x008800C6
	SRCINVERT  = 0x00660046
	CAPTUREBLT = 0x40000000
	PATCOPY    = 0x00F00021
	BLACKNESS  = 0x00000042
	WHITENESS  = 0x00FF0062
)

const (
	DIB_RGB_COLORS = 0
	BI_RGB         = 0
)

const (
	ULW_COLORKEY = 0x00000001
	ULW_ALPHA    = 0x00000002
	ULW_OPAQUE   = 0x00000004
	AC_SRC_OVER  = 0x00
	AC_SRC_ALPHA = 0x01
)

const (
	DT_LEFT         = 0x00000000
	DT_CENTER       = 0x00000001
	DT_RIGHT        = 0x00000002
	DT_TOP          = 0x00000000
	DT_VCENTER      = 0x00000004
	DT_BOTTOM       = 0x00000008
	DT_SINGLELINE   = 0x00000020
	DT_NOCLIP       = 0x00000100
	DT_CALCRECT     = 0x00000400
	DT_NOPREFIX     = 0x00000800
	DT_END_ELLIPSIS = 0x00008000
)

const (
	TRANSPARENT         = 1
	OPAQUE              = 2
	DEFAULT_CHARSET     = 1
	FW_NORMAL           = 400
	FW_SEMIBOLD         = 600
	FW_BOLD             = 700
	CLEARTYPE_QUALITY   = 5
	ANTIALIASED_QUALITY = 4
	DEFAULT_PITCH       = 0
	FF_DONTCARE         = 0
)

const (
	DWM_EXTENDED_FRAME_BOUNDS   = 9
	DWM_CLOAKED                 = 14
	DWM_USE_IMMERSIVE_DARK_MODE = 20
)

const (
	MONITOR_DEFAULTTONULL    = 0
	MONITOR_DEFAULTTOPRIMARY = 1
	MONITOR_DEFAULTTONEAREST = 2
)

const (
	NIM_ADD     = 0x00000000
	NIM_MODIFY  = 0x00000001
	NIM_DELETE  = 0x00000002
	NIF_MESSAGE = 0x00000001
	NIF_ICON    = 0x00000002
	NIF_TIP     = 0x00000004
	NIF_INFO    = 0x00000010
	NIIF_INFO   = 0x00000001
)

const (
	MF_STRING       = 0x00000000
	MF_SEPARATOR    = 0x00000800
	MF_CHECKED      = 0x00000008
	MF_GRAYED       = 0x00000001
	MF_DEFAULT      = 0x00001000
	TPM_RIGHTBUTTON = 0x0002
	TPM_RETURNCMD   = 0x0100
	TPM_NONOTIFY    = 0x0080
)

const (
	GMEM_FIXED    = 0x0000
	GMEM_MOVEABLE = 0x0002
	GMEM_ZEROINIT = 0x0040
)

const (
	CF_TEXT        = 1
	CF_DIB         = 8
	CF_DIBV5       = 17
	CF_UNICODETEXT = 13
)

const (
	MB_OK              = 0x00000000
	MB_ICONERROR       = 0x00000010
	MB_ICONINFORMATION = 0x00000040
	MB_TOPMOST         = 0x00040000
	MB_SETFOREGROUND   = 0x00010000
)

const (
	SPI_GETWORKAREA = 0x0030
	SPI_SETWORKAREA = 0x002F
)

// GDI+ enums
const (
	SmoothingModeInvalid     = -1
	SmoothingModeDefault     = 0
	SmoothingModeHighSpeed   = 1
	SmoothingModeHighQuality = 2
	SmoothingModeNone        = 3
	SmoothingModeAntiAlias   = 4
)

const (
	PixelOffsetModeInvalid     = -1
	PixelOffsetModeDefault     = 0
	PixelOffsetModeHighSpeed   = 1
	PixelOffsetModeHighQuality = 2
	PixelOffsetModeNone        = 3
	PixelOffsetModeHalf        = 4
)

const (
	PenAlignmentCenter                = 0
	LineCapFlat                       = 0
	LineCapRound                      = 2
	LineJoinRound                     = 2
	UnitPixel                         = 2
	TextRenderingHintAntiAliasGridFit = 3
)

func LParamXY(lp uintptr) (int32, int32) {
	return int32(int16(lp & 0xFFFF)), int32(int16((lp >> 16) & 0xFFFF))
}

func MakeLParam(x, y int32) LPARAM {
	return LPARAM(uint32(uint16(x)) | uint32(uint16(y))<<16)
}

func Loword(v uintptr) uint16 { return uint16(v & 0xFFFF) }

func Hiword(v uintptr) uint16 { return uint16((v >> 16) & 0xFFFF) }

func UTF16Ptr(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		empty := ""
		p, _ = syscall.UTF16PtrFromString(empty)
	}
	return p
}

func UTF16ToString(buf []uint16) string {
	return syscall.UTF16ToString(buf)
}

func CopyUTF16(dst []uint16, s string) {
	src := syscall.StringToUTF16(s)
	n := len(src)
	if n > len(dst) {
		n = len(dst)
	}
	copy(dst[:n], src[:n])
	if n > 0 {
		dst[n-1] = 0
	}
}

func ptr(v unsafe.Pointer) uintptr { return uintptr(v) }

type PAINTSTRUCT struct {
	Hdc         HDC
	FErase      int32
	RcPaint     RECT
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}
