# 2026-09-23 Computer Use 滚轮落点与可滚动面板识别

## 背景

`computer_scroll` 早已存在（`seelebridge/tools/computer/input_windows.go` 的
`Scroll`，注入 `MOUSEEVENTF_WHEEL`），MCP 面也有 `scroll`。但真实使用时有两个
结构性缺口：

1. 滚轮作用于**指针下方的控件**，页面里往往同时有页面主体、侧栏列表、聊天记录区
   等多个可滚动面板——只给坐标，模型点错地方就滚错东西；
2. 截图只含视口内的像素，"屏幕外还有没有上下文"既不在画面里，也不在窗口列表里，
   滚完也无从判断是到头了还是压根没滚到可滚动区域。

审查结论：仓库内**没有任何面板识别能力**（全仓零 UI Automation 代码），滚轮只有
裸注入。本次补齐"识别 + 落点 + 回读"三件事。

## 交付

| 层 | 位置 | 内容 |
|---|---|---|
| 领域类型与纯函数 | `seelebridge/tools/computer/scrolltarget.go` | `ScrollTarget`/`ScrollAxis`、`NormalizeScrollTargets`、`PickScrollTargetAtPoint`、`LargestVerticalScrollTarget`；过滤/排序/点命中/边界派生都可跨平台单测 |
| 平台原语 | `seelebridge/tools/computer/uia_windows.go` | 手写 COM 客户端：`ElementFromHandle` → `FindAll(IsScrollPatternAvailable)` → 空手则 raw view 有界树遍历；`ScrollStateAtPoint` 用 `ElementFromPoint` + 祖先链回读落点面板 |
| 滚轮序列 | `click.go` + `input_windows.go` | `scrollNotches` 把 delta 拆成按格的 `WHEEL_DELTA` 事件（余数单独一次、上限 10 格），滚后留 120ms 稳定时间 |
| Seelex 工具 | `tools_scroll.go` + `tools_input.go` + `tools_schema.go` + `tools.go` | 新增只读 `computer_scroll_targets`（第 11 个工具）；`computer_scroll` 支持 `window`、缺省落到最大纵向面板中心、结果回读 `panel{moved,before_percent,percent,view_size,at_start,at_end}` |
| 权限/可见性 | `permission_policy.go`、`config/seele.yaml`、`policy.go`（未改：只读不属输入注入） | `computer_scroll_targets` 归 `ro` 组（不占 `rw_desktop` 位，子代理可见），规则显式 `ask` |
| MCP 面 | `mcp/main.go` | 同步 `scroll_targets` 工具 + `scroll` 结果附落点面板摘要 |

## 真机证据（只读探针）

`SEELEX_COMPUTER_DESKTOP_PROBE=1 go test ./seelebridge/tools/computer -run
TestDesktopProbeScrollTargets -v`（可用 `SEELEX_COMPUTER_PROBE_WINDOW` 指定窗口，
不聚焦）：

- Seelex GUI：4 个面板，最大的是对话区 `conversation`（percent=100、view_size≈7.35%，
  即"已到底、上方还有大量历史"），另有 `right-tabpanel` 与两条页签栏；
- Edge（DeepSeek 文档页）：页面主体 1 个（percent=100、view_size≈78.7%）；
- Qoder：`消息列表` 1 个（percent=100.44、view_size≈12.5%），快路径 134ms；
- VS Code：快路径只回 Monaco 列表行 → 触发兜底遍历（这是把"覆盖度触发"和
  `minScrollTargetArea` 加进判据的直接原因）。

滚轮真实生效（临时探针）：对 Qoder `消息列表` 中心注入 `delta=+120`，
回读 100.439% → 97.251%；再注入 `delta=-120` 精确回到 100.439%，面板
class/rect 全程一致。

## 关键取舍

- **只读**：UIA 只用来观测，滚轮始终走 `SendInput`，与真人操作同一条路径；不调用
  `Scroll`/`SetScrollPercent`，也不注册事件。
- **Chromium 懒构建**：属性条件 `FindAll` 冷启动时只看得到工具栏（同一窗口实测 1 个
  vs 3 个可滚动面板），VS Code 更是只回 Monaco 列表行。因此判据是"有没有覆盖窗口
  面积 20% 的主面板"：没有才跑有界树遍历（节点 ≤3000、深度 ≤32、≤3s）并合并结果。
- **只报面板不报滚动项**：面积 <4096px² 的元素不算滚轮目标（Monaco 行 170×22），
  否则虚拟列表会用几百个"行"淹没面板清单；点命中回读不受该门槛限制。
- **必须给上限**：UIA 是跨进程调用，遍历是 O(节点数) 次系统调用；整次查询超时 5s
  （必须大于遍历预算，否则树还没走完就被外层掐断）、滚轮回读 2s，超时返回显式错误
  而不是把工具调用挂死。
- **不编造状态**：回读拿不到面板时输出 `unavailable: ["scroll_panel"]` 并提示重新
  截图，而不是假装滚动成功。

## 验证

```text
gofmt -l .
go vet ./...
go build ./...
go test ./seelebridge/tools/... -count=1
go test . -count=1
```
