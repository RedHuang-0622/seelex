# WinHide

## 生态位

`internal/winhide` 为 GUI 应用在加载项目（git）、执行 bash 工具或沙箱命令时隐藏 Windows 子进程控制台窗口（`CREATE_NO_WINDOW`），避免弹出黑色终端窗口；非 Windows 平台为空操作。主要调用方：`workspace`、`seelebridge/worktree`、`seelebridge/tools`、`seelebridge/security`、`seelebridge/scheduler`。

## 职责与非职责

- 职责：对传入的 `exec.Cmd` 设置隐藏子进程窗口所需的 `SysProcAttr`。
- 非职责：不管理子进程生命周期，不做超时或输出捕获，也不修改命令 argv。

## 核心实现

按平台拆分文件，在编译期隔离 Windows 专属字段：

- `winhide.go`：包文档。
- `winhide_windows.go`：Windows 实现，设置 `HideWindow: true` 与 `CreationFlags: 0x08000000`。
- `winhide_other.go`：非 Windows 空操作。

## Review 指南

- 平台专属字段只允许出现在 `winhide_windows.go`；新增平台时沿用 `winhide_other.go` 的空操作模式。
- 调用方统一走 `winhide.Apply(cmd)`，不要自行设置 `SysProcAttr` 覆盖该能力。

## 测试与验证

```text
gofmt -l internal/winhide
go build ./...
CGO_ENABLED=0 GOOS=windows go build ./internal/winhide
CGO_ENABLED=0 GOOS=linux go build ./internal/winhide
CGO_ENABLED=0 GOOS=darwin go build ./internal/winhide
```

## 文件与函数索引

- `winhide_windows.go`
  - `func Apply(cmd *exec.Cmd)`：隐藏子进程控制台窗口（Windows）；其它平台无副作用。
- `winhide_other.go`
  - `func Apply(_ *exec.Cmd)`：在非 Windows 平台为空操作。
