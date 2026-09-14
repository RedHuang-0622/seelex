# 2026-09-14 computer use 正式接入 Seelex harness（工具族 + 媒体/随图 + 权限/可见性）

> 日期: 2026-09-14 | 范围: `seelebridge/tools/computer/tools*.go`（新）、
> `seelebridge/runtime_computer.go`（新）、`seelebridge/runtime_tools.go`、
> `seelebridge/runtime_image.go`、`seelebridge/tools/policy.go`、
> `sessionstore/media.go`、`config/seele.yaml`、模块 README 与根 README

## 一、要解决的问题

桌面 computer use 此前只有两条腿：

1. **原语层**（`seelebridge/tools/computer`）：截屏/鼠标/键盘/窗口，平台相关；
2. **MCP 面**（`computer/mcp`）：把原语暴露给 Codex 之类的外部宿主，图像以
   base64 内联在 MCP 工具结果里。

Seelex 自己的 agent 用不上它——`2026-09-13` 的 devlog 已把这件事列为
「Seelex 侧工具未落地」。本次补的就是这一层：让 Seelex 的模型能截屏、能看图、
能点键鼠，并且**图像不靠 base64 糊上下文**。

## 二、实现

### 1. 工具族（`seelebridge/tools/computer/tools*.go`）

十个工具，名字即稳定契约（权限规则、插件 include/exclude、可见性策略都按名字
匹配）：`computer_screenshot` / `computer_windows` / `computer_focus` /
`computer_click` / `computer_move` / `computer_drag` / `computer_scroll` /
`computer_type` / `computer_keys` / `computer_wait`。

分层是刻意的：`tools.go` 只放装配面与共享 helper，观察类在 `tools_view.go`，
输入类在 `tools_input.go`，schema 在 `tools_schema.go`。工具层**不依赖
seelebridge 上层**——注册面、媒体分区、随图队列都是 `Deps` 闭包注入，因此
原语可以注入假实现，参数与媒体语义在任何平台都能单测。

### 2. 图像通道：媒体分区 + 下一次请求随图

```text
computer_screenshot
  → CaptureShot（DPI 感知 / 虚拟桌面物理像素）
  → PNG（BestSpeed）→ 8 MiB 上限校验
  → Router.WriteMediaWorkspace(projectID, sessionID, item) → media:<hash>
  → imageattach 队列（带字节的 image FilePart）
  → 下一次模型请求：Wrapper 把画面作为一条 user 消息附上（Take 即清空）
```

- 工具结果只回引用与几何（ref / 区域 / 缩放 / 光标 / 前台窗口），不内联
  base64：宿主上下文只有 200k，不该为了一时便利浪费。
- 归属由**执行 ctx** 决定，项目解析三级：执行会话绑定 → 节点作用域的工作区
  （子代理/Plan 节点会话没有独立绑定，用 `NodeScope.WorkspaceID`）→ 默认项目
  `""`；刻意不读 Router 活跃写作用域，也不用 `MainSessionID` 兜底——并行执行
  期间视图可能已切走，用它们解析会把截图写进另一个项目（与子代理记录持久化
  踩过的是同一个坑）。随图队列按执行会话投递：子代理截的图进子代理自己的
  队列，画面送给真正干活的 agent。
- `sessionstore` 补的接缝是 `Router.WriteMediaWorkspace` /
  `ReadMediaWorkspace`（显式项目作用域，与 SaveCommitWorkspace 同一口径）；
  后端不支持媒体分区时返回 `ErrMediaUnsupported`，不静默降级。
- `imageattach` 的 ref 兜底路径也接上了真实媒体分区
  （`runtime_image.go` 注入 `Loader: r.loadSessionMedia`）：附件只带
  `media:<hash>` 时按会话读回字节。

### 3. 门控：开关、权限、可见性

| 闸门 | 行为 |
|---|---|
| 平台 | 非 Windows 不注册（`Supported()`；宁可不给，也不挂一串必然失败的摆设） |
| 环境 | `SEELEX_COMPUTER_USE=0/off/false/no` 整体关闭（无头/CI） |
| 权限 | `config/seele.yaml` `permission.rules` 逐次 allow/ask/deny：本仓库给 `computer_*` 写了显式规则（观察也 ask，`computer_wait` allow） |
| 可见性 | 输入注入类工具对子代理不可见（并行子代理共用一块桌面会互相打断）；观察类对子代理可见 |

## 三、验证

```text
go test ./seelebridge/tools/... -count=1        # 含工具层契约
go test ./seelebridge -run "Computer|Media" -count=1
go test ./sessionstore -count=1
```

- `tools_test.go`：十工具注册与 schema；截屏落盘（PNG 可解码、来源标记
  screenshot、文件名带时间戳）与随图载荷（ref/字节/尺寸一致）；`max_width`
  钳制、region 校验、字节上限；窗口过滤/上限/单项降级；点击坐标三种解析路径
  （显式坐标 / 窗口中心 / 缺参报错）与按钮、次数校验；拖拽/滚轮/文本/组合键/
  等待的参数边界。
- `runtime_computer_test.go`：`SEELEX_COMPUTER_USE` 解析；`RegisterBuiltins`
  注册整族且关闭时不注册；媒体按 (项目, 会话) 落盘、跨会话读不到、
  `Loader` 读回；随图队列按执行会话入队且不串会话；随图包装确实挂上了媒体
  Loader。
- `policy_test.go`：子代理可见 `computer_screenshot`/`computer_windows`/
  `computer_wait` + `bash`，不可见任何输入注入工具；主代理整族可见。
- 真机桌面冒烟（默认跳过）：

```text
$env:SEELEX_COMPUTER_DESKTOP_PROBE=1; go test ./seelebridge/tools/computer -run RealDesktop -v
真机截屏: 1024x576 scale=0.667 region={0 0 1536 864} cursor=(542,737) foreground=true bytes=81975
真机窗口: 虚拟桌面={0 0 1536 864} 总数=10 返回=5 前台=true
```

（`computer_windows` 缺省过滤不可见窗口：同一台机器上"有标题的顶层窗口"实测
428 个，全部列出来只会淹没有效信息；`include_hidden=true` 才全列。）

## 四、未做 / 下一步

- **工具结果里的图**：`ToolResult.Multimodal` 仍未由工具结果透出（模型看得到，
  会话卡片的"工具结果图"还没有）。接缝候选仍是
  `application/core/tool_hooks.go` 与
  `application/core/task_context/task_context_state.go` 的 `storeToolResultLocked`。
  **这条是媒体 GC 的前置条件**：`sessionstore` 的 `CollectMedia` 以
  `ToolResult.Multimodal` 为引用集，截屏现在只被工具结果文本里的 ref 提到，
  一旦把媒体 GC 接到会话收尾路径，未挂 Multimodal 的截图会被回收。
- **输入注入的会话级串行化**：当前靠"子代理不可见"规避并发；若将来要给子代理
  开放，需要先做桌面级互斥（一次只有一个 actor 在动键鼠）。
- **坐标精度链路**：未做 labelme 形态的坐标描述（`shapes[].points` + 图片尺寸），
  模型目前靠自己按 region/scale 换算。
- **非 Windows 实现**：macOS/Linux 仍是桩（`ErrUnsupported`），工具族也随之
  不注册。
