//go:build darwin

package computer

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// ── macOS 框架绑定（纯 Go，无 cgo）──────────────────────────────
//
// macOS 的桌面能力来自系统框架：CoreGraphics（截屏 / 光标 / 事件注入）、
// CoreFoundation（CFString / CFArray / CFDictionary 等容器）、ApplicationServices
// 里的 Accessibility（把窗口置前）。
//
// 为什么用 dlopen/dlsym 而不是 cgo：与 Linux 侧选纯 Go 的 xgb 同一条理由——
// 保住 CGO_ENABLED=0 的四平台交叉编译。purego 用 Go 侧生成的 ABI 蹦床调用 C
// 函数，不需要 C 编译器，因此 `make build`（CGO=0）里 darwin 也有完整实现，而
// 不是掉进裸 ErrUnsupported 的桩里。
//
// 边界：本文件只做「符号绑定 + 类型适配」。原语语义（区域裁剪、按下/释放成对、
// 滚轮分格、UTF-16 逐字注入）一律复用平台无关层（click.go / image.go），与
// Windows、Linux 两个后端保持同一套可单测的判据。
//
// 注意：纯 Go 调 C 没有编译器帮你校对原型，因此每个绑定的参数/返回类型都按
// 头文件手写；结构体（CGPoint / CGRect）在 Go 侧必须与 C 侧逐字节同布局。

// cgFloat 是 CGFloat：64 位 macOS 上是 double。
type cgFloat = float64

// cgPoint / cgSize / cgRect 必须与 C 侧布局逐字节一致：
// CGPoint{x,y}、CGSize{width,height}、CGRect{origin,size}。
type cgPoint struct {
	X cgFloat
	Y cgFloat
}

type cgSize struct {
	Width  cgFloat
	Height cgFloat
}

type cgRect struct {
	Origin cgPoint
	Size   cgSize
}

// CoreFoundation 常量。
const (
	kCFStringEncodingUTF8 = 0x08000100
	kCFNumberSInt32Type   = 3
	kCFNumberSInt64Type   = 4
	kCFNumberFloat64Type  = 6
)

// CoreGraphics 常量（见 CGWindowLevel.h / CGRemoteOperation.h / CGEventTypes.h /
// CGBitmapContext.h）。
const (
	cgWindowListOptionOnScreenOnly     = 1 << 0
	cgWindowListExcludeDesktopElements = 1 << 4
	cgNullWindowID                     = 0
	kCGHIDEventTap                     = 0
	kCGScrollEventUnitLine             = 1
	kCGMouseButtonLeft                 = 0
	kCGMouseButtonRight                = 1
	kCGMouseButtonCenter               = 2
	kCGEventLeftMouseDown              = 1
	kCGEventLeftMouseUp                = 2
	kCGEventRightMouseDown             = 3
	kCGEventRightMouseUp               = 4
	kCGEventMouseMoved                 = 5
	kCGEventLeftMouseDragged           = 6
	kCGEventRightMouseDragged          = 7
	kCGEventOtherMouseDown             = 25
	kCGEventOtherMouseUp               = 26
	kCGEventOtherMouseDragged          = 27
	kCGEventFlagMaskShift              = 1 << 17
	kCGEventFlagMaskControl            = 1 << 18
	kCGEventFlagMaskAlternate          = 1 << 19
	kCGEventFlagMaskCommand            = 1 << 20

	// CGWindowListCopyWindowInfo 的字典键是字符串常量，其取值与名字相同。
	cgWindowKeyNumber   = "kCGWindowNumber"
	cgWindowKeyName     = "kCGWindowName"
	cgWindowKeyOwnerPID = "kCGWindowOwnerPID"
	cgWindowKeyBounds   = "kCGWindowBounds"
	cgWindowKeyLayer    = "kCGWindowLayer"
	cgWindowKeyOnscreen = "kCGWindowIsOnscreen"

	// Accessibility 的属性/动作名同样是固定字符串。
	axWindowsAttribute = "AXWindows"
	axTitleAttribute   = "AXTitle"
	axRaiseAction      = "AXRaise"

	// CGBitmapInfo 位域。
	kCGBitmapByteOrderMask     = 0x7000
	kCGBitmapByteOrder32Little = 2 << 12
)

// 框架路径（dlopen 用）。
const (
	coreFoundationFrameworkPath      = "/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation"
	coreGraphicsFrameworkPath        = "/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics"
	applicationServicesFrameworkPath = "/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices"
)

// 框架函数绑定；由 darwinLoad 一次性填充。
var (
	darwinOnce    sync.Once
	darwinLoadErr error

	// CoreFoundation
	cfRelease                 func(ref uintptr)
	cfStringCreateWithCString func(alloc uintptr, cstr string, encoding uint32) uintptr
	cfStringGetCString        func(s uintptr, buffer *byte, size int64, encoding uint32) bool
	cfArrayGetCount           func(array uintptr) int64
	cfArrayGetValueAtIndex    func(array uintptr, index int64) uintptr
	cfDictionaryGetValue      func(dict uintptr, key uintptr) uintptr
	cfNumberGetValue          func(number uintptr, theType int32, value unsafe.Pointer) bool
	cfBooleanGetValue         func(boolean uintptr) bool
	cfDataGetBytePtr          func(data uintptr) unsafe.Pointer
	cfDataGetLength           func(data uintptr) int64
	cfGetTypeID               func(ref uintptr) uint64
	cfBooleanGetTypeID        func() uint64

	// CoreGraphics：显示与图像
	cgGetActiveDisplayList func(maxDisplays uint32, displays *uint32, count *uint32) int32
	cgDisplayBounds        func(display uint32) cgRect
	cgDisplayCreateImage   func(display uint32) uintptr
	cgImageGetWidth        func(image uintptr) uintptr
	cgImageGetHeight       func(image uintptr) uintptr
	cgImageGetBytesPerRow  func(image uintptr) uintptr
	cgImageGetBitsPerPixel func(image uintptr) uintptr
	cgImageGetBitmapInfo   func(image uintptr) uint32
	cgImageGetDataProvider func(image uintptr) uintptr
	cgDataProviderCopyData func(provider uintptr) uintptr

	// CoreGraphics：窗口列表
	cgWindowListCopyWindowInfo func(option uint32, relativeTo uint32) uintptr
	cgRectMakeWithDictionary   func(dict uintptr, rect *cgRect) bool

	// CoreGraphics：事件
	cgEventCreate             func(source uintptr) uintptr
	cgEventGetLocation        func(event uintptr) cgPoint
	cgEventCreateMouseEvent   func(source uintptr, mouseType uint32, point cgPoint, button uint32) uintptr
	cgEventCreateScrollWheel  func(source uintptr, units uint32, wheelCount uint32, wheel1 int32, wheel2 int32, wheel3 int32) uintptr
	cgEventCreateKeyboard     func(source uintptr, virtualKey uint16, keyDown bool) uintptr
	cgEventKeyboardSetUnicode func(event uintptr, length uintptr, text *uint16)
	cgEventSetFlags           func(event uintptr, flags uint64)
	cgEventSetType            func(event uintptr, eventType uint32)
	cgEventPost               func(tap uint32, event uintptr)
	cgWarpMouseCursorPosition func(point cgPoint) int32
	cgAssociateMouse          func(connected bool) int32

	// 屏幕录制授权（10.15+；老系统缺失视为已授权）
	cgPreflightScreenCaptureAccess func() bool
	cgRequestScreenCaptureAccess   func() bool

	// Accessibility（ApplicationServices）
	axIsProcessTrusted       func() bool
	axUIElementCreateApp     func(pid int32) uintptr
	axUIElementCopyAttribute func(element uintptr, attribute uintptr, value *uintptr) int32
	axUIElementPerformAction func(element uintptr, action uintptr) int32
)

// darwinBind 把一个符号绑到函数变量上；符号不存在返回 false（而不是 panic）——
// 老系统的框架缺新符号是常态，调用方据此决定"能力缺失"还是"直接不可用"。
func darwinBind(handle uintptr, name string, target any) bool {
	symbol, err := purego.Dlsym(handle, name)
	if err != nil || symbol == 0 {
		return false
	}
	purego.RegisterFunc(target, symbol)
	return true
}

// darwinLoad 打开框架并绑定符号（进程内一次，失败结果缓存）。
func darwinLoad() error {
	darwinOnce.Do(func() { darwinLoadErr = darwinOpenFrameworks() })
	return darwinLoadErr
}

func darwinOpenFrameworks() error {
	coreFoundation, err := purego.Dlopen(coreFoundationFrameworkPath, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return fmt.Errorf("computer: 加载 CoreFoundation 失败: %w", err)
	}
	coreGraphics, err := purego.Dlopen(coreGraphicsFrameworkPath, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return fmt.Errorf("computer: 加载 CoreGraphics 失败: %w", err)
	}
	applicationServices, err := purego.Dlopen(applicationServicesFrameworkPath, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return fmt.Errorf("computer: 加载 ApplicationServices 失败: %w", err)
	}

	for _, bind := range []struct {
		handle uintptr
		name   string
		target any
	}{
		{coreFoundation, "CFRelease", &cfRelease},
		{coreFoundation, "CFStringCreateWithCString", &cfStringCreateWithCString},
		{coreFoundation, "CFStringGetCString", &cfStringGetCString},
		{coreFoundation, "CFArrayGetCount", &cfArrayGetCount},
		{coreFoundation, "CFArrayGetValueAtIndex", &cfArrayGetValueAtIndex},
		{coreFoundation, "CFDictionaryGetValue", &cfDictionaryGetValue},
		{coreFoundation, "CFNumberGetValue", &cfNumberGetValue},
		{coreFoundation, "CFBooleanGetValue", &cfBooleanGetValue},
		{coreFoundation, "CFDataGetBytePtr", &cfDataGetBytePtr},
		{coreFoundation, "CFDataGetLength", &cfDataGetLength},
		{coreFoundation, "CFGetTypeID", &cfGetTypeID},
		{coreFoundation, "CFBooleanGetTypeID", &cfBooleanGetTypeID},

		{coreGraphics, "CGGetActiveDisplayList", &cgGetActiveDisplayList},
		{coreGraphics, "CGDisplayBounds", &cgDisplayBounds},
		{coreGraphics, "CGDisplayCreateImage", &cgDisplayCreateImage},
		{coreGraphics, "CGImageGetWidth", &cgImageGetWidth},
		{coreGraphics, "CGImageGetHeight", &cgImageGetHeight},
		{coreGraphics, "CGImageGetBytesPerRow", &cgImageGetBytesPerRow},
		{coreGraphics, "CGImageGetBitsPerPixel", &cgImageGetBitsPerPixel},
		{coreGraphics, "CGImageGetBitmapInfo", &cgImageGetBitmapInfo},
		{coreGraphics, "CGImageGetDataProvider", &cgImageGetDataProvider},
		{coreGraphics, "CGDataProviderCopyData", &cgDataProviderCopyData},

		{coreGraphics, "CGWindowListCopyWindowInfo", &cgWindowListCopyWindowInfo},
		{coreGraphics, "CGRectMakeWithDictionaryRepresentation", &cgRectMakeWithDictionary},

		{coreGraphics, "CGEventCreate", &cgEventCreate},
		{coreGraphics, "CGEventGetLocation", &cgEventGetLocation},
		{coreGraphics, "CGEventCreateMouseEvent", &cgEventCreateMouseEvent},
		{coreGraphics, "CGEventCreateScrollWheelEvent2", &cgEventCreateScrollWheel},
		{coreGraphics, "CGEventCreateKeyboardEvent", &cgEventCreateKeyboard},
		{coreGraphics, "CGEventKeyboardSetUnicodeString", &cgEventKeyboardSetUnicode},
		{coreGraphics, "CGEventSetFlags", &cgEventSetFlags},
		{coreGraphics, "CGEventSetType", &cgEventSetType},
		{coreGraphics, "CGEventPost", &cgEventPost},
		{coreGraphics, "CGWarpMouseCursorPosition", &cgWarpMouseCursorPosition},
		{coreGraphics, "CGAssociateMouseAndMouseCursorPosition", &cgAssociateMouse},

		// 可选：授权探针（10.15+）与 Accessibility（HIServices）
		{coreGraphics, "CGPreflightScreenCaptureAccess", &cgPreflightScreenCaptureAccess},
		{coreGraphics, "CGRequestScreenCaptureAccess", &cgRequestScreenCaptureAccess},
		{applicationServices, "AXIsProcessTrusted", &axIsProcessTrusted},
		{applicationServices, "AXUIElementCreateApplication", &axUIElementCreateApp},
		{applicationServices, "AXUIElementCopyAttributeValue", &axUIElementCopyAttribute},
		{applicationServices, "AXUIElementPerformAction", &axUIElementPerformAction},
	} {
		if !darwinBind(bind.handle, bind.name, bind.target) && darwinRequiredSymbol(bind.name) {
			return fmt.Errorf("computer: CoreGraphics/CoreFoundation 缺少必需符号 %s", bind.name)
		}
	}
	return nil
}

// darwinRequiredSymbol 报告某个符号是"缺了就不能用"，还是"缺了降级即可"。
// 授权探针与 Accessibility 属于后者：没有它们仍能截屏与注入输入，只是无法
// 主动请求授权、也无法把窗口置前。
func darwinRequiredSymbol(name string) bool {
	switch name {
	case "CGPreflightScreenCaptureAccess", "CGRequestScreenCaptureAccess",
		"AXIsProcessTrusted", "AXUIElementCreateApplication",
		"AXUIElementCopyAttributeValue", "AXUIElementPerformAction":
		return false
	default:
		return true
	}
}

// darwinProbe 报告当前进程能否用 macOS 的 CoreGraphics 桌面能力。
func darwinProbe() error {
	if err := darwinLoad(); err != nil {
		return err
	}
	if cgGetActiveDisplayList == nil || cgDisplayCreateImage == nil {
		return fmt.Errorf("computer: CoreGraphics 不可用")
	}
	return nil
}

// darwinCFString 造一个 UTF-8 的 CFString 作为字典键；调用方负责 CFRelease。
func darwinCFString(value string) uintptr {
	return cfStringCreateWithCString(0, value, kCFStringEncodingUTF8)
}

// darwinCFStringValue 把 CFString 读回 Go 字符串（失败返回空）。
func darwinCFStringValue(ref uintptr) string {
	if ref == 0 || cfStringGetCString == nil {
		return ""
	}
	buffer := make([]byte, 1024)
	if !cfStringGetCString(ref, &buffer[0], int64(len(buffer)), kCFStringEncodingUTF8) {
		return ""
	}
	end := 0
	for end < len(buffer) && buffer[end] != 0 {
		end++
	}
	return string(buffer[:end])
}

// darwinCFNumber 从 CFNumber 读一个整数值。
func darwinCFNumber(ref uintptr) (int64, bool) {
	if ref == 0 || cfNumberGetValue == nil {
		return 0, false
	}
	var value int64
	if !cfNumberGetValue(ref, kCFNumberSInt64Type, unsafe.Pointer(&value)) {
		var narrow int32
		if !cfNumberGetValue(ref, kCFNumberSInt32Type, unsafe.Pointer(&narrow)) {
			return 0, false
		}
		return int64(narrow), true
	}
	return value, true
}

// darwinCFBool 从 CFBoolean 读一个布尔值。
func darwinCFBool(ref uintptr) (bool, bool) {
	if ref == 0 || cfBooleanGetValue == nil || cfBooleanGetTypeID == nil || cfGetTypeID == nil {
		return false, false
	}
	if cfGetTypeID(ref) != cfBooleanGetTypeID() {
		return false, false
	}
	return cfBooleanGetValue(ref), true
}

// darwinDictionaryValue 按已知字符串键取字典里的值（键由本文件临时造 CFString）。
func darwinDictionaryValue(dict uintptr, key string) uintptr {
	keyRef := darwinCFString(key)
	if keyRef == 0 {
		return 0
	}
	defer cfRelease(keyRef)
	return cfDictionaryGetValue(dict, keyRef)
}
