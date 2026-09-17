# Buildinfo

## 生态位

`internal/buildinfo` 承载构建期注入的版本信息：

- `Version`：发布版本（`-ldflags "-X .../internal/buildinfo.Version=<tag>"`）。
- `DefaultFrontend`：默认前端（`tui`；桌面构建覆盖为 `gui`）。

`main.go` 中的 `Version` / `DefaultFrontend` 是该包的别名（保持
`-X main.Version` 注入兼容），发布脚本注入点在 `internal/buildinfo`。

## 数据流图

```mermaid
flowchart LR
    SCRIPT["构建 / 发布脚本"] -->|"-ldflags -X …/internal/buildinfo.Version=tag"| VAR["buildinfo.Version"]
    BUILD["构建标签"] --> FE["buildinfo.DefaultFrontend<br/>tui（默认）/ gui（桌面构建覆盖）"]
    VAR --> MAIN["main.go 别名（保持 -X main.Version 兼容）"]
    FE --> MAIN
    MAIN --> CLI["-version 输出 / 前端默认选择"]
    NOTE["源码构建报告 dev<br/>release 版本来自 Git tag"] -.-> VAR
```
