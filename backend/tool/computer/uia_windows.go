//go:build windows

package computer

import (
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

// ── UI Automation 客户端：识别页面里哪些面板可以滚轮操作 ──────
//
// 事实来源：Windows SDK 10.0.22621 的 `um/UIAutomationClient.h`。这里只用到
// 只读入口（不注册事件、不写 UI 状态）：
//
//	IUIAutomation::ElementFromHandle / ElementFromPoint / CreatePropertyCondition /
//	CreateTrueCondition / get_RawViewWalker
//	IUIAutomationElement::FindAll / GetCurrentPatternAs / get_CurrentBoundingRectangle
//	IUIAutomationElementArray::get_Length / GetElement
//	IUIAutomationTreeWalker::GetParentElement
//	IUIAutomationScrollPattern::get_Current*（滚动位置与视口比例）
//
// vtable 槽位（IUnknown 占 0-2）按头文件顺序硬编码，每一处都写明方法名：
// 手写 COM 调用最容易出错的就是"序号错一个"，改动前必须先对照头文件。
//
// 线程与单元：COM 接口有线程亲和性，而 Go 的 goroutine 会在 OS 线程间迁移。
// 因此每次查询都在锁定的线程上 CoInitializeEx(MTA) → CoCreateInstance → 用完
// CoUninitialize；接口指针只在这一次查询内使用，不跨调用复用。

var (
	ole32DLL    = syscall.NewLazyDLL("ole32.dll")
	oleaut32DLL = syscall.NewLazyDLL("oleaut32.dll")

	procCoInitializeEx   = ole32DLL.NewProc("CoInitializeEx")
	procCoUninitialize   = ole32DLL.NewProc("CoUninitialize")
	procCoCreateInstance = ole32DLL.NewProc("CoCreateInstance")
	procSysStringLen     = oleaut32DLL.NewProc("SysStringLen")
	procSysFreeString    = oleaut32DLL.NewProc("SysFreeString")
)

// UIA 常量（UIAutomationClient.h）。
const (
	uiaScrollPatternID                    = 10004
	uiaIsScrollPatternAvailablePropertyID = 30034
	coinitMultithreaded                   = 0x0
	clsctxInprocServer                    = 0x1
	treeScopeDescendants                  = 0x4
	variantTypeBool                       = 11
)

// vtable 槽位：IUIAutomation。
const (
	slotElementFromHandle       = 6
	slotElementFromPoint        = 7
	slotGetRawViewWalker        = 16
	slotCreateTrueCondition     = 21
	slotCreatePropertyCondition = 23
)

// vtable 槽位：IUIAutomationElement。
const (
	slotElementFindAll                     = 6
	slotElementGetCurrentPatternAs         = 14
	slotElementCurrentLocalizedControlType = 22
	slotElementCurrentName                 = 23
	slotElementCurrentAutomationID         = 29
	slotElementCurrentClassName            = 30
	slotElementCurrentIsOffscreen          = 38
	slotElementCurrentBoundingRectangle    = 43
)

// vtable 槽位：IUIAutomationElementArray / IUIAutomationTreeWalker /
// IUIAutomationScrollPattern。
const (
	slotArrayLength = 3
	slotArrayGet    = 4

	slotWalkerParent      = 3
	slotWalkerFirstChild  = 4
	slotWalkerNextSibling = 6

	slotScrollHorizontalPercent      = 5
	slotScrollVerticalPercent        = 6
	slotScrollHorizontalViewSize     = 7
	slotScrollVerticalViewSize       = 8
	slotScrollHorizontallyScrollable = 9
	slotScrollVerticallyScrollable   = 10
)

// maxScrollAncestorDepth 是一次点命中回读里最多往上走几层祖先。
const maxScrollAncestorDepth = 32

// 兜底树遍历的上限：节点数、深度、时间。三样都要有上限——UIA 的跨进程遍历在
// 大页面上是 O(节点数) 次调用，没有上限就等于把一次工具调用挂死。
const (
	scrollTargetWalkNodeBudget = 3000
	scrollTargetWalkMaxDepth   = 32
	scrollTargetWalkTimeout    = 3 * time.Second
	// minScrollTargetArea 是"值得问一句能不能滚"的最小面积（约 64×64 像素）：比它更
	// 小的元素不读 ScrollPattern（省两次跨进程调用），也不作为面料列给模型——
	// Chromium 的虚拟列表会把每一行都报成可滚动元素（实测 VS Code 的 Monaco 行
	// 170×22=3740px²），那些是滚动**项**不是可滚轮操作的面板。
	minScrollTargetArea = 4096
	// mainPanelCoverage 是"快路径已经看到主面板"的面积门槛：最大候选不到窗口面积的
	// 这个比例时，认为可访问性树还没长齐（Chromium 懒构建），再跑一遍树遍历。
	mainPanelCoverage = 0.2
)

// S_OK / E_FAIL：手写 COM 调用只关心"是不是 0"。
const (
	comSOK   = 0
	comEFail = 0x80004005
)

type comGUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

// CLSID_CUIAutomation / IID_IUIAutomation / IID_IUIAutomationScrollPattern。
var (
	clsidCUIAutomation = comGUID{
		Data1: 0xFF48DBA4, Data2: 0x60EF, Data3: 0x4201,
		Data4: [8]byte{0xAA, 0x87, 0x54, 0x10, 0x3E, 0xEF, 0x59, 0x4E},
	}
	iidIUIAutomation = comGUID{
		Data1: 0x30CBE57D, Data2: 0xD9D0, Data3: 0x452A,
		Data4: [8]byte{0xAB, 0x13, 0x7A, 0xC5, 0xAC, 0x48, 0x25, 0xEE},
	}
	iidIUIAutomationScrollPattern = comGUID{
		Data1: 0x88F4D42A, Data2: 0xE881, Data3: 0x459D,
		Data4: [8]byte{0xA7, 0x7C, 0x73, 0xBB, 0xBB, 0x7E, 0x02, 0xDC},
	}
)

// comRect 对应 Win32 RECT。
type comRect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

// comVariant 对应 Win32 VARIANT（x64 为 24 字节）。这里只用来构造
// CreatePropertyCondition 需要的 VT_BOOL 条件值。
type comVariant struct {
	VT    uint16
	_     [3]uint16
	Value uintptr
	Extra uintptr
}

func boolVariant(value bool) comVariant {
	variant := comVariant{VT: variantTypeBool}
	if value {
		variant.Value = ^uintptr(0) // VARIANT_TRUE = -1
	}
	return variant
}

func hresultError(action string, hr uintptr) error {
	return fmt.Errorf("computer: %s 失败: 0x%08X", action, uint32(hr))
}

// comCall 调用 COM 接口的第 index 个 vtable 槽位（IUnknown 占 0-2）。
//
// 接口指针用 unsafe.Pointer 承载（不是 uintptr）：uintptr→unsafe.Pointer 的
// 反向转换会被 go vet 判为"possible misuse of unsafe.Pointer"，而对 C 侧对象
// 本来就不该那样写。
func comCall(object unsafe.Pointer, index int, args ...uintptr) uintptr {
	if object == nil {
		return comEFail
	}
	table := *(*unsafe.Pointer)(object)
	method := *(*uintptr)(unsafe.Add(table, uintptr(index)*unsafe.Sizeof(uintptr(0))))
	callArgs := make([]uintptr, 0, len(args)+1)
	callArgs = append(callArgs, uintptr(object))
	callArgs = append(callArgs, args...)
	ret, _, _ := syscall.SyscallN(method, callArgs...)
	return ret
}

// comRelease 释放一个 COM 接口指针（IUnknown::Release，槽位 2）。
func comRelease(object unsafe.Pointer) {
	if object == nil {
		return
	}
	comCall(object, 2)
}

// withUIAutomation 在锁定的 OS 线程上建立一次 COM 会话并创建 IUIAutomation。
func withUIAutomation(fn func(automation unsafe.Pointer) error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hr, _, _ := procCoInitializeEx.Call(0, coinitMultithreaded)
	initialized := hr == 0 || hr == 1 // S_OK / S_FALSE（本线程已初始化）
	switch {
	case initialized:
		defer procCoUninitialize.Call()
	case uint32(hr) == 0x80010106: // RPC_E_CHANGED_MODE：本线程已在别的单元里
	default:
		return hresultError("CoInitializeEx", hr)
	}

	clsid := clsidCUIAutomation
	iid := iidIUIAutomation
	var automation unsafe.Pointer
	hr, _, _ = procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsid)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&automation)),
	)
	runtime.KeepAlive(&clsid)
	runtime.KeepAlive(&iid)
	if hr != comSOK || automation == nil {
		return hresultError("CoCreateInstance(CUIAutomation)", hr)
	}
	defer comRelease(automation)
	return fn(automation)
}

// withUIATimeout 给一次 UIA 查询加超时：UIA 是跨进程调用，目标进程卡住时调用方
// 会被拖住。超时后调用方拿到显式错误，而不是把自己也挂死。
func withUIATimeout(timeout time.Duration, query func() error) error {
	if timeout <= 0 {
		timeout = DefaultScrollTargetTimeout
	}
	done := make(chan error, 1)
	go func() {
		done <- query()
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("computer: UI Automation 查询超时（%s）", timeout)
	}
}

// ListScrollTargets 枚举一个窗口里可以滚轮操作的面板，供模型判断"屏幕外的
// 上下文在哪个面板里、还差多少"。只读：不点、不滚、不聚焦。
func (win32Desktop) ListScrollTargets(opts ScrollTargetOptions) ([]ScrollTarget, error) {
	var targets []ScrollTarget
	err := withUIATimeout(opts.Timeout, func() error {
		found, err := listScrollTargetsOnce(opts)
		targets = found
		return err
	})
	if err != nil {
		return nil, err
	}
	return NormalizeScrollTargets(targets, 0), nil
}

// ScrollStateAtPoint 报告"这一点上的滚轮会滚到哪个面板"，以及该面板当前的滚动
// 位置。找不到可滚动面板时返回 found=false（不是错误：那块地方本来就不滚动）。
func (win32Desktop) ScrollStateAtPoint(p Point, timeout time.Duration) (ScrollTarget, bool, error) {
	var (
		target ScrollTarget
		found  bool
	)
	err := withUIATimeout(timeout, func() error {
		result, ok, err := scrollStateAtPointOnce(p)
		target, found = result, ok
		return err
	})
	return target, found, err
}

func listScrollTargetsOnce(opts ScrollTargetOptions) ([]ScrollTarget, error) {
	handle := opts.WindowHandle
	if handle == 0 {
		foreground, _, err := procGetForegroundWindow.Call()
		if foreground == 0 {
			return nil, fmt.Errorf("computer: 无法获取前台窗口: %v", err)
		}
		handle = foreground
	}
	var targets []ScrollTarget
	err := withUIAutomation(func(automation unsafe.Pointer) error {
		root, err := uiaElementFromHandle(automation, handle)
		if err != nil {
			return err
		}
		defer comRelease(root)
		targets, err = findScrollTargets(automation, root, opts.IncludeOffscreen)
		return err
	})
	if err != nil {
		return nil, err
	}
	return targets, nil
}

func scrollStateAtPointOnce(p Point) (ScrollTarget, bool, error) {
	var (
		target ScrollTarget
		found  bool
	)
	err := withUIAutomation(func(automation unsafe.Pointer) error {
		element, err := uiaElementFromPoint(automation, p)
		if err != nil {
			return err
		}
		walker, walkerErr := uiaRawViewWalker(automation)
		if walkerErr != nil {
			walker = nil
		}
		current := element
		defer func() {
			comRelease(current)
			comRelease(walker)
		}()
		for depth := 0; depth < maxScrollAncestorDepth; depth++ {
			// 点命中回读不接受面积门槛：指针落在哪个面板上就报哪个面板。
			if candidate, ok := readScrollTargetElement(current, true, 0); ok {
				target, found = candidate, true
				return nil
			}
			if walker == nil {
				return nil
			}
			parent, _ := uiaParentElement(walker, current)
			if parent == nil {
				return nil // 走到树根就停：这不是"查询失败"
			}
			comRelease(current)
			current = parent
		}
		return nil
	})
	return target, found, err
}

// findScrollTargets 找出窗口子树里的可滚动面板。
//
// 快路径：按 UIA_IsScrollPatternAvailable 属性条件 FindAll，只让 UIA 返回带
// ScrollPattern 的元素（实测 Chrome/Edge 约 200ms 拿到 3 个候选）。
//
// 兜底路径：Chromium 的可访问性树是**懒构建**的——冷启动时 FindAll 只能看到
// 工具栏/列表行那一小块（同一窗口实测 1 个 vs 3 个可滚动面板；VS Code 实测
// 只回 Monaco 列表行，拿不到编辑器主区）。因此在**快路径没有看到"主面板"**时
// （最大候选面积 < 窗口面积的 20%，或一个都没看到），再用有界的 raw view 深度
// 优先遍历兜底并把两批结果合并：遍历本身会促使宿主把树补齐，同时也回答了
// "这个窗口真的没有可滚动面板"。跑得动、但不盲目：主面板一旦在场就不再遍历。
func findScrollTargets(automation, root unsafe.Pointer, includeOffscreen bool) ([]ScrollTarget, error) {
	targets := fastScrollTargets(automation, root, includeOffscreen)
	if windowRect, ok := uiaElementRect(root); !ok || !hasMainPanel(targets, windowRect) {
		walked, walkErr := walkScrollTargetsBounded(automation, root, includeOffscreen)
		if walkErr != nil {
			if len(targets) == 0 {
				return nil, walkErr
			}
			return targets, nil
		}
		targets = mergeScrollTargets(targets, walked)
	}
	return targets, nil
}

// fastScrollTargets 走属性条件 FindAll：UIA 只返回带 ScrollPattern 的元素。
func fastScrollTargets(automation, root unsafe.Pointer, includeOffscreen bool) []ScrollTarget {
	condition, conditionErr := uiaPatternAvailableCondition(automation, uiaIsScrollPatternAvailablePropertyID)
	if conditionErr != nil {
		return nil
	}
	array, findErr := uiaFindAll(root, treeScopeDescendants, condition)
	comRelease(condition)
	if findErr != nil {
		return nil
	}
	defer comRelease(array)
	return readScrollTargetArray(array, includeOffscreen, minScrollTargetArea)
}

// hasMainPanel 报告候选里是否已经有"够大"的面板：窗口矩形不可知（面积 0）时只
// 要求非空，否则要求至少一个候选覆盖到窗口面积的 20%。
func hasMainPanel(targets []ScrollTarget, windowRect Rect) bool {
	if len(targets) == 0 {
		return false
	}
	windowArea := windowRect.Width * windowRect.Height
	if windowArea <= 0 {
		return true
	}
	threshold := int(float64(windowArea) * mainPanelCoverage)
	for _, target := range targets {
		if target.Area() >= threshold {
			return true
		}
	}
	return false
}

// mergeScrollTargets 合并快路径与遍历两批结果，按矩形+控件类型去重（同一块面板
// 两条路都会看到，重复列出只会挤掉别的候选）。
func mergeScrollTargets(primary, secondary []ScrollTarget) []ScrollTarget {
	merged := make([]ScrollTarget, 0, len(primary)+len(secondary))
	seen := map[string]bool{}
	for _, batch := range [][]ScrollTarget{primary, secondary} {
		for _, target := range batch {
			key := fmt.Sprintf("%d,%d,%d,%d|%s|%s|%s",
				target.Rect.X, target.Rect.Y, target.Rect.Width, target.Rect.Height,
				target.ControlType, target.ClassName, target.Name)
			if seen[key] {
				continue
			}
			seen[key] = true
			merged = append(merged, target)
		}
	}
	return merged
}

// walkScrollTargetsBounded 跑一次有界 raw view 遍历，返回收集到的面板。
func walkScrollTargetsBounded(automation, root unsafe.Pointer, includeOffscreen bool) ([]ScrollTarget, error) {
	walker, err := uiaRawViewWalker(automation)
	if err != nil {
		return nil, err
	}
	defer comRelease(walker)
	state := &scrollTargetWalk{deadline: time.Now().Add(scrollTargetWalkTimeout)}
	walkScrollTargets(walker, root, 0, state, includeOffscreen)
	return state.targets, nil
}

// scrollTargetWalk 是遍历的可变状态（节点预算 + 截止时间 + 已收集结果）。
type scrollTargetWalk struct {
	targets  []ScrollTarget
	visited  int
	deadline time.Time
}

func (w *scrollTargetWalk) exhausted() bool {
	return w.visited >= scrollTargetWalkNodeBudget ||
		len(w.targets) >= maxScrollTargetResults ||
		time.Now().After(w.deadline)
}

// walkScrollTargets 深度优先遍历子树，收集带 ScrollPattern 的元素。
func walkScrollTargets(walker, element unsafe.Pointer, depth int, state *scrollTargetWalk, includeOffscreen bool) {
	if element == nil || state.exhausted() || depth > scrollTargetWalkMaxDepth {
		return
	}
	state.visited++
	if target, ok := readScrollTargetElement(element, includeOffscreen, minScrollTargetArea); ok {
		state.targets = append(state.targets, target)
	}
	child, err := uiaWalkerStep(walker, element, slotWalkerFirstChild)
	for child != nil && err == nil {
		walkScrollTargets(walker, child, depth+1, state, includeOffscreen)
		next, nextErr := uiaWalkerStep(walker, child, slotWalkerNextSibling)
		comRelease(child)
		child, err = next, nextErr
	}
}

// readScrollTargetArray 逐元素读取面板信息；读到上限即停（网页里的滚动容器
// 数量有限，但子树规模可能很大，必须有硬上限）。
func readScrollTargetArray(array unsafe.Pointer, includeOffscreen bool, minArea int) []ScrollTarget {
	length := uiaArrayLength(array)
	targets := make([]ScrollTarget, 0, 8)
	for index := 0; index < length && len(targets) < maxScrollTargetResults; index++ {
		element, err := uiaArrayElement(array, index)
		if err != nil {
			continue
		}
		target, ok := readScrollTargetElement(element, includeOffscreen, minArea)
		comRelease(element)
		if ok {
			targets = append(targets, target)
		}
	}
	return targets
}

// readScrollTargetElement 把一个 UIA 元素读成滚动面板：没有 ScrollPattern、
// 面积过小、离屏或矩形无效都返回 false。
func readScrollTargetElement(element unsafe.Pointer, includeOffscreen bool, minArea int) (ScrollTarget, bool) {
	if element == nil {
		return ScrollTarget{}, false
	}
	rect, ok := uiaElementRect(element)
	if !ok || rect.Width <= 0 || rect.Height <= 0 || rect.Width*rect.Height < minArea {
		return ScrollTarget{}, false
	}
	if !includeOffscreen && uiaElementOffscreen(element) {
		return ScrollTarget{}, false
	}
	pattern, err := uiaScrollPattern(element)
	if err != nil || pattern == nil {
		return ScrollTarget{}, false
	}
	defer comRelease(pattern)

	target := ScrollTarget{
		Rect:         rect,
		Name:         uiaElementString(element, slotElementCurrentName),
		ControlType:  uiaElementString(element, slotElementCurrentLocalizedControlType),
		AutomationID: uiaElementString(element, slotElementCurrentAutomationID),
		ClassName:    uiaElementString(element, slotElementCurrentClassName),
		Vertical: readScrollAxis(pattern,
			slotScrollVerticallyScrollable, slotScrollVerticalPercent, slotScrollVerticalViewSize),
		Horizontal: readScrollAxis(pattern,
			slotScrollHorizontallyScrollable, slotScrollHorizontalPercent, slotScrollHorizontalViewSize),
	}
	if !target.Scrollable() {
		return ScrollTarget{}, false
	}
	return target, true
}

func uiaPatternAvailableCondition(automation unsafe.Pointer, propertyID uintptr) (unsafe.Pointer, error) {
	value := boolVariant(true)
	var condition unsafe.Pointer
	hr := comCall(automation, slotCreatePropertyCondition,
		propertyID, uintptr(unsafe.Pointer(&value)), uintptr(unsafe.Pointer(&condition)))
	runtime.KeepAlive(&value)
	if hr != comSOK || condition == nil {
		return nil, hresultError("CreatePropertyCondition", hr)
	}
	return condition, nil
}

func uiaTrueCondition(automation unsafe.Pointer) (unsafe.Pointer, error) {
	var condition unsafe.Pointer
	hr := comCall(automation, slotCreateTrueCondition, uintptr(unsafe.Pointer(&condition)))
	if hr != comSOK || condition == nil {
		return nil, hresultError("CreateTrueCondition", hr)
	}
	return condition, nil
}

func uiaRawViewWalker(automation unsafe.Pointer) (unsafe.Pointer, error) {
	var walker unsafe.Pointer
	hr := comCall(automation, slotGetRawViewWalker, uintptr(unsafe.Pointer(&walker)))
	if hr != comSOK || walker == nil {
		return nil, hresultError("get_RawViewWalker", hr)
	}
	return walker, nil
}

func uiaElementFromHandle(automation unsafe.Pointer, handle uintptr) (unsafe.Pointer, error) {
	var element unsafe.Pointer
	hr := comCall(automation, slotElementFromHandle, handle, uintptr(unsafe.Pointer(&element)))
	if hr != comSOK || element == nil {
		return nil, hresultError("ElementFromHandle", hr)
	}
	return element, nil
}

func uiaElementFromPoint(automation unsafe.Pointer, p Point) (unsafe.Pointer, error) {
	var element unsafe.Pointer
	// POINT 是 8 字节结构体，x64 调用约定把它塞进一个寄存器槽位：低位 X、高位 Y。
	point := uintptr(uint32(int32(p.X))) | uintptr(uint32(int32(p.Y)))<<32
	hr := comCall(automation, slotElementFromPoint, point, uintptr(unsafe.Pointer(&element)))
	if hr != comSOK || element == nil {
		return nil, hresultError("ElementFromPoint", hr)
	}
	return element, nil
}

func uiaFindAll(root unsafe.Pointer, scope uintptr, condition unsafe.Pointer) (unsafe.Pointer, error) {
	var array unsafe.Pointer
	hr := comCall(root, slotElementFindAll, scope, uintptr(condition), uintptr(unsafe.Pointer(&array)))
	if hr != comSOK || array == nil {
		return nil, hresultError("FindAll", hr)
	}
	return array, nil
}

func uiaParentElement(walker, element unsafe.Pointer) (unsafe.Pointer, error) {
	var parent unsafe.Pointer
	hr := comCall(walker, slotWalkerParent, uintptr(element), uintptr(unsafe.Pointer(&parent)))
	if hr != comSOK || parent == nil {
		return nil, hresultError("GetParentElement", hr)
	}
	return parent, nil
}

// uiaWalkerStep 调用 TreeWalker 的子/兄弟遍历（slot 取 slotWalkerFirstChild 等）。
// 走到末尾时返回 (nil, nil)：那是正常的树边界，不是错误。
func uiaWalkerStep(walker, element unsafe.Pointer, slot int) (unsafe.Pointer, error) {
	var next unsafe.Pointer
	hr := comCall(walker, slot, uintptr(element), uintptr(unsafe.Pointer(&next)))
	if hr != comSOK || next == nil {
		return nil, hresultError("TreeWalker::Step", hr)
	}
	return next, nil
}

func uiaArrayLength(array unsafe.Pointer) int {
	var length int32
	hr := comCall(array, slotArrayLength, uintptr(unsafe.Pointer(&length)))
	if hr != comSOK || length < 0 {
		return 0
	}
	return int(length)
}

func uiaArrayElement(array unsafe.Pointer, index int) (unsafe.Pointer, error) {
	var element unsafe.Pointer
	hr := comCall(array, slotArrayGet, uintptr(index), uintptr(unsafe.Pointer(&element)))
	if hr != comSOK || element == nil {
		return nil, hresultError("ElementArray::GetElement", hr)
	}
	return element, nil
}

// readScrollAxis 读一条轴的状态：不可滚动的轴不给位置数值（百分点是"内容里的
// 相对位置"，不可滚动时报 UnknownScrollValue 才符合 UIA 口径）。
func readScrollAxis(pattern unsafe.Pointer, scrollableSlot, percentSlot, viewSizeSlot int) ScrollAxis {
	axis := ScrollAxis{Percent: UnknownScrollValue, ViewSize: UnknownScrollValue}
	axis.Scrollable = uiaBool(pattern, scrollableSlot)
	if !axis.Scrollable {
		return axis
	}
	if percent, ok := uiaDouble(pattern, percentSlot); ok {
		axis.Percent = percent
	}
	if viewSize, ok := uiaDouble(pattern, viewSizeSlot); ok {
		axis.ViewSize = viewSize
	}
	return axis
}

func uiaScrollPattern(element unsafe.Pointer) (unsafe.Pointer, error) {
	iid := iidIUIAutomationScrollPattern
	var pattern unsafe.Pointer
	hr := comCall(element, slotElementGetCurrentPatternAs,
		uiaScrollPatternID, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&pattern)))
	runtime.KeepAlive(&iid)
	if hr != comSOK || pattern == nil {
		return nil, hresultError("GetCurrentPatternAs(ScrollPattern)", hr)
	}
	return pattern, nil
}

func uiaElementRect(element unsafe.Pointer) (Rect, bool) {
	var rect comRect
	hr := comCall(element, slotElementCurrentBoundingRectangle, uintptr(unsafe.Pointer(&rect)))
	if hr != comSOK {
		return Rect{}, false
	}
	return Rect{
		X:      int(rect.Left),
		Y:      int(rect.Top),
		Width:  int(rect.Right - rect.Left),
		Height: int(rect.Bottom - rect.Top),
	}, true
}

func uiaElementOffscreen(element unsafe.Pointer) bool {
	var offscreen int32
	hr := comCall(element, slotElementCurrentIsOffscreen, uintptr(unsafe.Pointer(&offscreen)))
	return hr == comSOK && offscreen != 0
}

// uiaElementString 读一个 BSTR 属性，读完立刻 SysFreeString（UIA 把字符串所有权
// 交给调用方，漏掉就是跨进程字符串泄漏）。
func uiaElementString(element unsafe.Pointer, slot int) string {
	var raw unsafe.Pointer
	hr := comCall(element, slot, uintptr(unsafe.Pointer(&raw)))
	if hr != comSOK || raw == nil {
		return ""
	}
	defer procSysFreeString.Call(uintptr(raw))
	length, _, _ := procSysStringLen.Call(uintptr(raw))
	if length == 0 {
		return ""
	}
	return syscall.UTF16ToString(unsafe.Slice((*uint16)(raw), int(length)))
}

func uiaBool(object unsafe.Pointer, slot int) bool {
	var value int32
	hr := comCall(object, slot, uintptr(unsafe.Pointer(&value)))
	return hr == comSOK && value != 0
}

func uiaDouble(object unsafe.Pointer, slot int) (float64, bool) {
	var value float64
	hr := comCall(object, slot, uintptr(unsafe.Pointer(&value)))
	if hr != comSOK {
		return 0, false
	}
	return value, true
}
