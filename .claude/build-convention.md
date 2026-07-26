# 构建产物输出规范

## 输出路径

所有编译产物必须输出到项目根目录下的 `dist/<os>-<arch>/` 目录，**不是**项目根目录。

| 目标 | 输出路径 |
|------|---------|
| Windows GUI | `dist/windows-amd64/seelex-gui.exe` |
| Windows TUI | `dist/windows-amd64/seelex-tui.exe` |
| Linux TUI | `dist/linux-amd64/seelex` |
| macOS GUI | `dist/darwin-amd64/seelex-gui` / `dist/darwin-arm64/seelex-gui` |
| macOS TUI | `dist/darwin-amd64/seelex` / `dist/darwin-arm64/seelex` |

## 构建命令

### 本地开发（单平台）

```bash
# GUI 版本（完整 Wails 构建）
go build -tags "gui desktop production" -ldflags "-X main.DefaultFrontend=gui" -o dist/windows-amd64/seelex-gui.exe .

# TUI 版本
go build -o dist/windows-amd64/seelex-tui.exe .
```

### 多平台发布（使用 Makefile）

```bash
make build VERSION=x.y.z    # 构建所有平台到 dist/<os>-<arch>/
make package VERSION=x.y.z  # 构建 + 打包为 tar.gz
make clean                  # 清理整个 dist/
```

## 构建约束

- **GUI 版本**：需要三个参数配合，缺一不可
  - `-tags "gui desktop production"` — `gui` = 项目自定义标签（切换 `run_wails.go`/`run_stub.go`），`desktop` + `production` = Wails v2 运行时必需标签
  - `-ldflags "-X main.DefaultFrontend=gui"` — 将 `main.DefaultFrontend` 从 `"tui"` 覆盖为 `"gui"`
- 前端资源：`gui/frontend/dist/*` 通过 `gui/assets.go` 的 `//go:embed` 编译进 GUI 二进制
- 跨平台：`CGO_ENABLED=0` + `GOOS`/`GOARCH` 环境变量

## 前端资源更新

修改 `gui/frontend/dist/` 下的 HTML/JS/CSS 后，**必须重新编译**才能生效：

```bash
go build -tags "gui desktop production" -ldflags "-X main.DefaultFrontend=gui" -o dist/windows-amd64/seelex-gui.exe .
```

因为前端文件通过 `//go:embed` 在编译时嵌入二进制，无法热更新。

## 为什么用 `dist/` 而不是根目录

- `dist/` 是 Makefile 约定的标准输出目录
- 避免根目录被编译产物污染
- `make clean` 一键清理所有产物
- 多平台产物按目录隔离，方便打包分发
