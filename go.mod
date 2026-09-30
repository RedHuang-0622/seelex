module github.com/RedHuang-0622/seelex

go 1.25.8

// Seele：发布依赖走下面的 require，**无 replace**（纯净依赖）。版本链：
// v0.3.1 = Linux 式权限模型（主体×路由组×rwx + sudo 与中间件判定）+
// session.InLoop 环内历史把手；v0.3.2（2026-09-28）= 方案 B：以「回合闸门 +
// 短临界区工作状态」替换 InLoop 把手（session/inloop.go 整条删除，History 永不
// 阻塞、回合内写历史经检查点排队）。
//
// 三次本地 replace 联调（2026-09-15 权限模型、2026-09-26 InLoop、2026-09-28
// 方案 B）都在对应 tag 发布后移除，回归纯净依赖；宿主侧对方案 B 的迁移
// （不再持有 inloop 把手、写历史改走检查点排队）已随本次升版一并落地。
//
// go-pty（v0.2.3）：GUI 下栏终端的跨平台 PTY。Windows 走 ConPTY，unix 走
// creack/pty，是唯一被维护的纯 Go 跨平台伪终端实现；自研 ConPTY 需要 unsafe
// 组装 STARTUPINFOEX/PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE，风险高于依赖本身。
// 它只在 gui/terminal 使用（桌面宿主），unix 构建才会连带编译 u-root 的
// termios（其 ssh 支持所需），Windows 不受影响。

require (
	github.com/RedHuang-0622/Seele v0.3.2
	github.com/atotto/clipboard v0.1.4
	github.com/aymanbagabas/go-pty v0.2.3
	github.com/charmbracelet/bubbles v1.0.0
	github.com/charmbracelet/bubbletea v1.3.10
	github.com/charmbracelet/lipgloss v1.1.0
	github.com/ebitengine/purego v0.11.1
	github.com/google/uuid v1.6.0
	github.com/jezek/xgb v1.3.1
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2
	github.com/wailsapp/wails/v2 v2.13.0
	gopkg.in/yaml.v3 v3.0.1
)

// M0（2026-10-01）：jobs 根能力先在 Seele 本地检出联调，故暂加 replace 指向
// G:/Program/go/seele。Seele 打 tag 发布 jobs 后本段即删除，回归纯净依赖
// （与三次本地 replace 联调的既有纪律一致）。
replace github.com/RedHuang-0622/Seele => G:/Program/go/seele

require (
	git.sr.ht/~jackmordaunt/go-toast/v2 v2.0.3 // indirect
	github.com/RedHuang-0622/TemplatePoolByGO v0.1.8 // indirect
	github.com/RedHuang-0622/microHub v0.1.5 // indirect
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/bep/debounce v1.2.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/charmbracelet/colorprofile v0.4.1 // indirect
	github.com/charmbracelet/x/ansi v0.11.6 // indirect
	github.com/charmbracelet/x/cellbuf v0.0.15 // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/clipperhouse/displaywidth v0.9.0 // indirect
	github.com/clipperhouse/stringish v0.1.1 // indirect
	github.com/clipperhouse/uax29/v2 v2.5.0 // indirect
	github.com/creack/pty v1.1.24 // indirect
	github.com/erikgeiser/coninput v0.0.0-20211004153227-1c3628e74d0f // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.4.0 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/google/jsonschema-go v0.4.2 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/jchv/go-winloader v0.0.0-20210711035445-715c2860da7e // indirect
	github.com/labstack/echo/v4 v4.13.3 // indirect
	github.com/labstack/gommon v0.4.2 // indirect
	github.com/leaanthony/go-ansi-parser v1.6.1 // indirect
	github.com/leaanthony/gosod v1.0.4 // indirect
	github.com/leaanthony/slicer v1.6.0 // indirect
	github.com/leaanthony/u v1.1.1 // indirect
	github.com/lucasb-eyer/go-colorful v1.3.0 // indirect
	github.com/mark3labs/mcp-go v0.54.0 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-localereader v0.0.1 // indirect
	github.com/mattn/go-runewidth v0.0.19 // indirect
	github.com/muesli/ansi v0.0.0-20230316100256-276c6243b2f6 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/muesli/termenv v0.16.0 // indirect
	github.com/pelletier/go-toml/v2 v2.2.4 // indirect
	github.com/pkg/browser v0.0.0-20240102092130-5ac0b6a4141c // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/sagikazarmark/locafero v0.11.0 // indirect
	github.com/samber/lo v1.49.1 // indirect
	github.com/sourcegraph/conc v0.3.1-0.20240121214520-5f936abd7ae8 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/spf13/viper v1.21.0 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	github.com/tkrajina/go-reflector v0.5.8 // indirect
	github.com/u-root/u-root v0.16.0 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasttemplate v1.2.2 // indirect
	github.com/wailsapp/go-webview2 v1.0.22 // indirect
	github.com/wailsapp/mimetype v1.4.1 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	go.opentelemetry.io/otel/trace v1.44.0 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/crypto v0.53.0 // indirect
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.39.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/grpc v1.83.1 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)
