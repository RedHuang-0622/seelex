//go:build linux

package computer

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
)

// 本文件是 Linux（X11）后端的地基：连接生命周期、原子/属性读写、把输入事件
// 注入 X 服务器的统一出口。原语分散在 screen_linux.go（截图与光标）、
// input_linux.go（鼠标与键盘）、window_linux.go（窗口枚举与激活）。
//
// 为什么直接说 X11 协议而不是叫 xdotool/scrot：与 Windows 侧直接用
// user32/gdi32 对称——不引入运行期外部二进制依赖，且 xgb 是纯 Go，
// CGO_ENABLED=0 的四平台交叉编译不受影响。
//
// 已知边界：XDG_SESSION_TYPE=wayland 时 DISPLAY 通常由 XWayland 提供，注入只
// 能作用于 X11 客户端窗口（原生 Wayland 窗口看不到，除非以后另做
// xdg-desktop-portal 后端）。VM 实测是 Xorg 会话，因此这里不做 Wayland 分支。

// errX11NoDisplay 表示没有可用的 X11 会话（无头 / CI）。
var errX11NoDisplay = errors.New("computer: 未设置 DISPLAY（没有 X11 会话）")

// x11Session 是一次 X11 连接及其派生状态；所有原语都在 x11Mu 下使用它。
// xgb.Conn 本身支持并发请求，但错误会从"下一次有回包的请求"里冒出来，
// 因此这里把整段操作放在一把锁下，让成功/失败的对应关系保持确定。
type x11Session struct {
	conn   *xgb.Conn
	setup  *xproto.SetupInfo
	screen xproto.ScreenInfo
	root   xproto.Window
	format xproto.Format
	visual xproto.VisualInfo
	order  binary.ByteOrder
	atoms  map[string]xproto.Atom
	keys   *x11Keymap
}

var (
	x11Mu        sync.Mutex
	x11Connected *x11Session
)

// x11Use 在互斥下执行 op；连接惰性建立，连接级故障后丢弃会话以便下次重连。
// op 内**不得**再调用 x11Use（会自锁）。
func x11Use(op func(*x11Session) error) error {
	x11Mu.Lock()
	defer x11Mu.Unlock()
	if x11Connected == nil {
		session, err := x11Open()
		if err != nil {
			return err
		}
		x11Connected = session
	}
	err := op(x11Connected)
	if err != nil && x11ConnectionBroken(err) {
		x11Connected.conn.Close()
		x11Connected = nil
	}
	return err
}

// x11Probe 尝试建立一次连接（成功后缓存在会话里），失败原因原样返回。
func x11Probe() error {
	return x11Use(func(*x11Session) error { return nil })
}

// x11ConnectionBroken 判断错误是否意味着连接已经不可用：协议错误（BadWindow
// 之类）说明"这次请求不合法"，连接还能用；传输层错误（EOF、broken pipe）
// 才需要丢掉连接重来。
func x11ConnectionBroken(err error) bool {
	var protocolErr xgb.Error
	return !errors.As(err, &protocolErr)
}

func x11Open() (*x11Session, error) {
	display := strings.TrimSpace(os.Getenv("DISPLAY"))
	if display == "" {
		return nil, errX11NoDisplay
	}
	conn, err := xgb.NewConn()
	if err != nil {
		return nil, fmt.Errorf("computer: 连接 X11 失败（DISPLAY=%s）: %w", display, err)
	}
	setup := xproto.Setup(conn)
	if setup == nil || len(setup.Roots) == 0 {
		conn.Close()
		return nil, fmt.Errorf("computer: X11 setup 没有可用屏幕（DISPLAY=%s）", display)
	}
	index := conn.DefaultScreen
	if index < 0 || index >= len(setup.Roots) {
		index = 0
	}
	screen := setup.Roots[index]
	session := &x11Session{
		conn:   conn,
		setup:  setup,
		screen: screen,
		root:   screen.Root,
		atoms:  map[string]xproto.Atom{},
	}
	session.format, session.visual = rootPixelFormat(setup, screen)
	if setup.ImageByteOrder == xproto.ImageOrderMSBFirst {
		session.order = binary.BigEndian
	} else {
		session.order = binary.LittleEndian
	}
	// XTEST 是输入注入的唯一通道：扩展缺失就没有 computer use 的输入面。
	// 与其注册一串必然失败的工具，不如在连接阶段就判定不可用。
	if err := xtest.Init(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("computer: X11 缺少 XTEST 扩展（无法注入鼠标与键盘）: %w", err)
	}
	return session, nil
}

// rootPixelFormat 找出根窗口深度的 ZPixmap 格式与对应 visual（通道掩码）。
func rootPixelFormat(setup *xproto.SetupInfo, screen xproto.ScreenInfo) (xproto.Format, xproto.VisualInfo) {
	var format xproto.Format
	for _, candidate := range setup.PixmapFormats {
		if candidate.Depth == screen.RootDepth {
			format = candidate
			break
		}
	}
	var visual xproto.VisualInfo
	for _, depth := range screen.AllowedDepths {
		if depth.Depth != screen.RootDepth {
			continue
		}
		for _, candidate := range depth.Visuals {
			if candidate.VisualId == screen.RootVisual {
				visual = candidate
			}
		}
	}
	if visual.BitsPerRgbValue == 0 && len(screen.AllowedDepths) > 0 && len(screen.AllowedDepths[0].Visuals) > 0 {
		// 极少数服务器不给根 visual 掩码：退回第一个可用 visual。
		visual = screen.AllowedDepths[0].Visuals[0]
	}
	return format, visual
}

// atom 取（并缓存）一个 X11 原子。
func (s *x11Session) atom(name string) (xproto.Atom, error) {
	if atom, ok := s.atoms[name]; ok {
		return atom, nil
	}
	reply, err := xproto.InternAtom(s.conn, false, uint16(len(name)), name).Reply()
	if err != nil {
		return 0, fmt.Errorf("computer: 查询 X11 原子 %s 失败: %w", name, err)
	}
	s.atoms[name] = reply.Atom
	return reply.Atom, nil
}

// property 读一个属性，返回类型原子、format 与原始值；属性不存在时返回
// (0, 0, nil, nil)。
func (s *x11Session) property(window xproto.Window, atom xproto.Atom, length uint32) (xproto.Atom, byte, []byte, error) {
	reply, err := xproto.GetProperty(s.conn, false, window, atom, xproto.AtomAny, 0, length).Reply()
	if err != nil {
		return 0, 0, nil, fmt.Errorf("computer: 读取 X11 属性失败: %w", err)
	}
	if reply.Format == 0 {
		return 0, 0, nil, nil
	}
	return reply.Type, reply.Format, reply.Value, nil
}

// propertyOf 按属性名读原始值；属性不存在时返回 nil。
func (s *x11Session) propertyOf(window xproto.Window, name string, length uint32) ([]byte, error) {
	atom, err := s.atom(name)
	if err != nil {
		return nil, err
	}
	_, _, value, err := s.property(window, atom, length)
	return value, err
}

// atomList 把 format=32 的属性值解码成 id 列表（按服务器字节序）。
func (s *x11Session) atomList(value []byte) []uint32 {
	count := len(value) / 4
	out := make([]uint32, 0, count)
	for index := 0; index < count; index++ {
		out = append(out, s.order.Uint32(value[index*4:]))
	}
	return out
}

// atomListOf 读窗口上某个属性的 32 位 id 列表（属性不存在返回 nil）。
func (s *x11Session) atomListOf(window xproto.Window, name string, length uint32) ([]uint32, error) {
	value, err := s.propertyOf(window, name, length)
	if err != nil {
		return nil, err
	}
	return s.atomList(value), nil
}

// fakeKey / fakeMotion / fakeButton 是输入注入的唯一出口，全部走 checked 请求：
// 注入端必须能报告真实成功/失败，调用方才可能在"按下之后失败"时补一次释放
// （见 click.go 里那次真实事故的说明）。
func (s *x11Session) fakeKey(eventType byte, keycode byte) error {
	if err := xtest.FakeInputChecked(s.conn, eventType, keycode, 0, 0, 0, 0, 0).Check(); err != nil {
		return fmt.Errorf("computer: 注入键盘事件失败: %w", err)
	}
	return nil
}

func (s *x11Session) fakeMotion(point Point) error {
	if err := xtest.FakeInputChecked(s.conn, xproto.MotionNotify, 0, 0, s.root,
		int16(point.X), int16(point.Y), 0).Check(); err != nil {
		return fmt.Errorf("computer: 移动指针失败: %w", err)
	}
	return nil
}

func (s *x11Session) fakeButton(eventType byte, button byte, point Point) error {
	if err := xtest.FakeInputChecked(s.conn, eventType, button, 0, s.root,
		int16(point.X), int16(point.Y), 0).Check(); err != nil {
		return fmt.Errorf("computer: 注入鼠标按键事件失败: %w", err)
	}
	return nil
}
