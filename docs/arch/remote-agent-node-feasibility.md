# 远程 agent 节点（personal Agent 的员工节点）可行性与暴露面评估

状态：**评估 + 提案。本文未改代码。** 结论全部来自对仓内代码/文档的静读；凡"已确认"都带
`文件`/`文件:符号` 锚点，凡"假设"都显式标注并给出可复现判据。

一句话：**可行，而且依赖倒置留下的缝足够用；但远程边界必须落在 `RuntimePort` 之下**——
把大端口远程化会被它自己的通道型成员与进程内所有权钉死。

---

## 1. 结论

| 问题 | 结论 |
|---|---|
| "Seelex 作为 personal Agent，把一个远端机器当员工节点用" | **可行**，且不需要新造抽象——现有三处缝已够（§2） |
| 最小的可用形态 | **L0：把 `computer_*` 原语经 SSE/ssh 暴露成远端 MCP**，改动量最小、今天就能跑（§5） |
| 架构上最"正"的落点 | **L2：workunit 作业面/现场/会话端口的 remote 实现**——"一个 Work Item 的执行面 = 远端节点" |
| 不该做的 | **L3：远程化 `RuntimePort` / `ChatEngine`**（理由见 §4 #2） |
| 桌面类节点的载体 | **VM 池做灰度/金丝雀 + 至少一台真机做对照**，不是二选一（§8） |

---

## 2. 依赖倒置留下的三处缝（当前实现）

### 2.1 契约在消费方，装配在组合根

- 端口：`contract/ports.go`（`ChatEngine` / `RuntimePort` / `PluginPort` /
  `SkillPort` / `SessionPort` / `WorkspacePort` / `ApprovalBroker` …）。
- 实现：`internal/adapters/`、`seelebridge/`、`store/`、`workspace/`、`plugin/`。
- 唯一装配点：`application/application.go` 的 `New(deps Dependencies) (*Service, error)`；
  组合根是 `main.go`。
- 更严的一版：`seelebridge/workunit/contract.go` 的三格端口（作业面 / 现场 / 会话记录），
  唯一实现是 `seelebridge/workunit_assembly.go` 的 `hostPorts`，并有机械门禁
  `e2e/workunit_ports_test.go`（契约包 import 实现包零命中）。

### 2.2 员工执行面本身就是可替换的（本次问题的正面回答）

```go
// seelebridge/runtime_role_turn.go:52
// roleEngine 是一个角色会话的引擎最小面。*session.Session 满足它；
// 测试可注入假实现（角色回合的验收不需要真实 LLM）。
type roleEngine interface {
    SessionID() string
    SetSystemPrompt(prompt string)
    SetMaxLoops(n int)
    ClearHistory()
    ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error)
    History() []types.Message
}
```

注入点：`seelebridge/runtime_role_turn.go:216`
`func (r *Runtime) SetRoleEngineFactory(factory func(sessionID string) (roleEngine, error))`；
`seelebridge/README.md` 自陈用途之一是"**更严隔离面**"。

**这就是"远程员工节点"最干净的落点**：写一个 `remoteRoleEngine` 把 `ChatStream` 转发到另一台
机器上的 Seelex，一个员工的工作回合就从"本机进程内引擎"变成"远端节点"。该接口只有 6 个方法、
**不含任何 channel**、只有一处流式回调——它是可被远程化的。

### 2.3 装配矩阵已预留"换一个作业面实现"

`docs/arch/workunit-ports-and-assembly.md` §3 的形态矩阵里明确一行：
`（未来的）换一个作业面实现 | 换实现，其余不动`。§6 的形态表也已列了 `只读巡检员` 这类新形态。

---

## 3. computer use 本来就是"跨进程可用"的

- `seelebridge/tools/computer/mcp/` 是**一个 stdio MCP 服务端**，明确服务于"外部宿主（Codex 之类）"；
  原语层与"图像怎么进模型"解耦：Seelex 自己走会话媒体分区（`media:<hash>` 随下一次请求送），
  MCP 面走 base64 内联（见 `seelebridge/tools/computer/README.md` 的两消费面对照表）。
- Seelex 作为 MCP **客户端**已支持远端：
  `vendor/github.com/RedHuang-0622/Seele/tools/mcp/provider.go:108-114`
  `case "stdio": NewStdioMCPClient(...)` / `case "sse": NewSSEMCPClient(cfg.URL)`；
  配置面 `config/mcp.example.yaml` 的 `transport: stdio | sse` + `url`。
- 装配闸门：`seelebridge/runtime_computer.go`（`bridgecomputer.Supported()` +
  `SEELEX_COMPUTER_USE`），因此"注册不注册这族工具"是一个显式开关。
- 权威实测方法学与判据：`docs/2026-09-25-linux-vm-demo/computer-use.md`（含 `xev` / `xdotool` 独立裁判法）。

---

## 4. 真正的缺口（是活，不是接线）

| # | 缺口 | 证据 | 影响 |
|---|---|---|---|
| 1 | **Seelex 自己的包没有任何网络服务端** | `seelebridge/ application/ internal/ gui/ tui/ plugin/ mcpstack/ main.go` 下 `net.Listen` / `ListenAndServe` 零命中；`NewMCPServer` 全仓零命中；唯一的"服务端"是 `computer-mcp` 那个 stdio JSON-RPC | 远端节点要写服务端，或走 ssh 隧道复用 stdio |
| 2 | **通道型端口不能跨进程** | `RuntimePort` 含 `AsyncRunEvents() <-chan struct{}`、`TaskChangedChannel() <-chan dto.TaskRecord`、`SubagentTreeEvents() <-chan struct{}`、`PlanNodeEventChannel() <-chan dto.PlanNodeEvent`、`SubscribeSubagentLive() (…, <-chan …, func(), error)`；`TeamworkJobCompletion.TeamworkJobEvents()` 同理 | **这是"别远程化 RuntimePort"的硬理由**；它另有几十个 `X` / `XFor(sessionID)` 成对方法，全带会话级内存所有权与锁纪律 |
| 3 | **权限是进程内的** | 主体写在调度 ctx 上、不落共享字段（`docs/arch/agent-permission-subjects.md` §2.1）；`emp_<role>` 是本地权责表条目（`seelebridge/tools/permission_policy.go:194` `DefaultPermissionSubjects`） | 跨机之后需要**第二道**门："谁可以驱动这个节点"。现有员工权限管不到这条边界 |
| 4 | **数据根是单进程锁** | `store/data_root_lock.go` + `data_root_lock_multiprocess_test.go` | 多节点**必须各自 data root**，汇聚只能走网络。这与"隔离"同向，不是障碍 |
| 5 | **员工桌面输入位为零** | 见 §4.1 | "让员工随便用桌面"要重新裁决权限口径 |

### 4.1 员工今天在桌面上的实际权责（代码确认）

`seelebridge/tools/permission_policy.go:194-232` 的 `DefaultPermissionSubjects`：

| 主体 | ro | rw(project) | rw_session | **rw_desktop** | ctl | adm |
|---|---|---|---|---|---|---|
| `root` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ |
| `sub` | ✔ | ✔ | — | **—** | — | — |
| `emp_ro` | ✔ | — | — | **—** | — | — |
| `emp_rw` | ✔ | ✔ | — | **—** | — | — |

`GroupRWDesktop` 在 `employeeRO` / `employeeRW` 里**整个字段都没有**（不是置 0，是没有这一项）。
配合 `docs/arch/agent-permission-subjects.md` §2.2 的路由分组：

- 桌面**观察**类（`computer_screenshot` / `computer_windows` / `computer_scroll_targets`）路由到 `ro`
  → 员工位齐，但组默认 `ask` → **每次都要宿主点一下**；
- 桌面**输入**类（`click` / `type` / `scroll` / `drag` / `press_keys`）路由到 `rw_desktop`
  → **员工没有位**。

§4 的口径是员工位缺 → 走"执行选择页面提权"（宿主的人类就是 sudo 口令）；但 §7 自标"可见性面"
未落地（位缺当前表现为 dispatch 时 `ErrToolNotVisible`，而非"不在工具清单里"）。

> 口径：**今天"方便员工 computer use"实际是"方便员工看屏幕"**；动鼠标敲键盘要么走 `root`，
> 要么每次人工点头。这是产品决定，不是接线问题。

---

## 5. 暴露阶梯（最小可用优先）

```text
L0  computer_* 原语 ──(SSE / ssh+stdio)──▶ 远端桌面节点    ← 今天可做，不碰核心代码
L1  窄接口：提交回合 / 观察会话 / 作业三动作(observe·fetch·kill) ──▶ 远端 Seelex 节点
L2  workunit 作业面·现场·会话端口的 remote 实现 ──▶ 一个 Work Item 的执行面 = 远端节点
L3  不做：远程化 RuntimePort / ChatEngine
```

L0 的暴露面即**已经写死在契约里的工具名 + JSON 参数**
（`computer_screenshot` / `windows` / `scroll_targets` / `cursor` / `wait` / `move` / `click` /
`drag` / `scroll` / `type`），没有新契约要设计。

**L0 必须先拍的一颗钉子**：MCP 工具在权责模型里走 `adm` 组（`mcp_load` = "改变能力面本身"，
默认 `ask`），**不是** `rw_desktop`。于是"远端桌面做成 MCP server 挂进来"会把权限语义从
"每次桌面输入问人"退化成"装不装这个 MCP 问一次"。要么接受这个粗粒度，要么给远端节点工具补一条
路由组。**不先裁决这条，L0 的权限面就是错位的。**

---

## 6. 红线

1. **远端节点必须驱动一块专用隔离桌面**，绝不是用户正在用的那块屏。`rw_desktop` 的资源限定注释
   原文：`写共享外设（一块桌面，所有并行代理共用）`（`docs/arch/agent-permission-subjects.md` §2.2）。
   "隔离员工的电脑"不是过度设计，是这个模型的必然要求。
2. **节点侧要有"谁可以驱动我"的门**。节点内部的 subject × group × bits 解决不了跨机边界
   （那是进程内的），需要另做一层（token / 来源绑定 / 只监听环回 + 隧道）。
3. **节点 ↔ leader 的契约要版本化**。`application.ProtocolVersion` 是既有先例。

---

## 7. 平台事实：三个桌面后端的能力对照（**含对早期误判的更正**）

早期口径曾说"macOS 是三个后端里能力面最全的"。**这是错的**，按代码更正如下
（`seelebridge/tools/computer/` 下各 `platform_*.go`）：

| 能力 | Windows | macOS | Linux(X11) |
|---|---|---|---|
| 桌面观察/输入（`Desktop`） | ✔ | ✔ | ✔ |
| `computer_scroll_targets` | ✔（UI Automation 的 ScrollPattern） | **✘ 恒 false**（`platform_darwin.go` 注释：AXScrollArea/AXScrollBar 未实现） | **✘ 设计性缺失**（X11 协议只有窗口与像素） |
| 非 ASCII 文本注入（中文/emoji） | ✔ `SendInput` 逐 UTF-16 | ✔ `CGEventKeyboardSetUnicodeString` 直送 UTF-16 | **✘ 已知缺陷**：临时重映射键码丢字串字（`docs/2026-09-25-linux-vm-demo/computer-use.md` §4） |
| 前提/门槛 | Per-Monitor V2 DPI 感知声明（否则 125% 缩放下坐标落 80%） | 「屏幕录制」TCC 授权，**且授权后需重启 Seelex**（`platform_darwin.go:Prepare`） | 桌面输入法须在英文态；Wayland 未覆盖 |

**所以"Mac 绷得住"目前有代码依据的只有一条：打字这个环节。**
`input_darwin.go` 的 `TypeText` 注释原文：*"macOS 没有 X11 那种'临时重映射键码'的负担：
CGEventKeyboardSetUnicodeString 直接把任意 UTF-16 序列送给前台应用"* —— Linux 那个丢字串字的
缺陷在 macOS 上是**结构性不存在**的，不是"碰巧没复现"。

**macOS 的代价同样在代码里**：`Prepare()` 只预检「屏幕录制」授权，未授权直接报错且要求重启进程；
而`input_darwin.go` 的 `darwinPostMouse` 注释自陈 *"CGEventPost 无返回，系统级投放失败是静默的"*
——**输入那条路的 TCC 失败是静默的**。因此任何 macOS 金丝雀都**必须包含"注入真的生效了"的
外部核验**（照 `docs/2026-09-25-linux-vm-demo/computer-use.md` 的裁判法：外部工具/落盘文件读回），不能靠工具自证。

---

## 8. 灰度 / 金丝雀的载体：VM 池 还是 真买 Mac Studio

### 8.1 这两个不是二选一

**正确答案是"VM 池做金丝雀 + 至少一台真机做对照"。** 它们回答的是两个不同的问题：
VM 回答"能不能跑、坏了能不能退"；真机回答"Retina / 多屏 / 真实 TCC 下面还对不对"。

### 8.2 为什么 macOS 上 VM 几乎是唯一选择

- macOS 没有容器这条路。Linux 侧你已经有 `docker/linux-gui.Dockerfile`（容器化的 Linux GUI），
  macOS 侧对应物不存在 → **VM 是 macOS 上唯一能拿到"可弃置环境"的手段**。
- VM 的杀手好处正是金丝雀要的形状：base image = 「已登录图形会话 + 已授权 TCC + 固定分辨率」的
  标准件；每个版本一份克隆；坏了秒级回滚；能克隆 N 份做**并行**灰度。
- 与本仓架构天然对齐：节点各自持有 data root（§4 #4 的 `data_root_lock` 约束）→ **灰度单位
  天然就是"节点"**，VM 池正好生产"节点"。

### 8.3 VM 的四个盲区（有代码依据）

1. **Retina backing scale 折算路径走不到**。`screen_darwin.go` 的整套坐标语义围绕
   `darwinBlitDisplay` 的 `ratioX = source.Dx() / display.Width`（把 Retina 像素按 backing scale
   采样回**点**空间）设计。Virtualization.framework 的虚拟显示器通常 scale = 1 → **ratio 恒 1，
   这条折算路径在 VM 里根本不会被执行**。而真机接 Retina 屏是 2x。
2. **多显示器并集 / 负坐标原点走不到**。`darwinUnionBounds` 与 `VirtualScreen()` 的口径是
   "多显示器并集，左上角可能为负"；单虚拟屏 VM 不触发。
3. **真实 TCC 生命周期被掩盖**。VM 里 TCC 状态随快照走/随克隆复制，可能掩盖"首次授权 /
   二进制路径或签名变化 / 授权后重启"这些真机才暴露的问题。
4. **（假设，待验）无头 VM 的虚拟显示器是否在线**：`darwinActiveDisplays()` 返回 0 时
   `VirtualScreen()` 报 **"没有在线显示器"**。若 headless 跑 VM 时虚拟显示器离线，
   "无人值守金丝雀"这条路直接断。**这是第一个该测的判据**（见 §9）。

### 8.4 买 Studio 的判据

Studio 的买点**不是"能跑 macOS VM"**（任何 Apple Silicon Mac 都能），而是：

- **VM 密度**：同时跑几个 guest（内存 ÷ 每 guest 内存、核心数）；
- **长时间满载**：散热 / 功耗 / 噪音 / 稳定性。

只需"1 个金丝雀 + 1 个基线" → Mac mini 级别足够，Studio 是浪费；
要做"多节点并行灰度"（例如 5+ 节点）→ Studio 才有理由。

### 8.5 许可前提（**未核实，落地前确认**）

macOS 虚拟化按 Apple 的软件许可只允许运行在 Apple 品牌硬件上，且对可同时运行的额外实例数
有限制。**本条我没有核过许可原文，也不在仓内任何文档里**——采购前请自行确认。

---

## 9. 最小第一步（可验收，零采购起步）

用现有 Apple Silicon Mac 当宿主，起 1 个 macOS VM（Tart / UTM 之类，均基于
Virtualization.framework），跑三个判据：

| # | 判据 | 为什么它决定设计 |
|---|---|---|
| 1 | **headless VM** 下 `computer_windows` / `VirtualScreen` 是否报"没有在线显示器" | 决定"无人值守金丝雀"成不成立（§8.3 #4） |
| 2 | TCC 授权在**克隆出的** VM 里是否保留 | 决定能不能做"预制标准件"（决定 VM 池可不可行） |
| 3 | 换新版本二进制后是否需要重新授权 / 重启 | 决定灰度一次部署的人工成本 |

判据 2、3 通过后，再跑一次远端驱动 spike（照 `docs/2026-09-25-linux-vm-demo/computer-use.md` §6 的剧本，
外部核验而不是工具自证）：SSE 隧道（或 ssh + stdio）挂上 `computer-mcp`，主 Seelex 用
`config/mcp.yaml` 的 `sse` 条目接入，观察延迟 / 断线重连 / 截图进上下文的成本 /
**权限退化成 `adm` 之后的实际手感**（§5 的那颗钉子）。

---

## 10. 已确认 / 假设 / 未验证

**已确认（有代码或文档锚点）**

- 依赖倒置三处缝：`contract/ports.go`、`seelebridge/runtime_role_turn.go:52,216`、
  `docs/arch/workunit-ports-and-assembly.md` §3。
- `computer-mcp` 是 stdio MCP 服务端；MCP 客户端支持 SSE 远端（vendor `provider.go:108-114`）。
- Seelex 自己的包无网络服务端（`net.Listen` / `ListenAndServe` / `NewMCPServer` 零命中）。
- `RuntimePort` 的 5 处 `<-chan` 成员。
- 员工 `rw_desktop` 位为零；员工只读观察也要逐次 `ask`。
- macOS `ScrollTargets` 恒 false；macOS 文本注入无需重映射键码。
- 数据根单进程锁。

**假设（需实测）**

- 无头 macOS VM 的虚拟显示器是否在线（§8.3 #4）——判据见 §9 #1。
- macOS guest 能否在 SSH/daemon 上下文注入 CGEvent（依赖 Aqua 会话）。

**未验证**

- macOS computer use 后端的**整体可用性**：仓内只有实现文件，**没有 macOS 实测报告**
  （唯一实测报告是 Linux X11 的 `docs/2026-09-25-linux-vm-demo/computer-use.md`）。
- Apple 虚拟化许可的实例数限制原文。
- 员工提权路径的生产可用性：`docs/arch/agent-permission-subjects.md` §7 自标部分未落地，
  且该文档 2026-09-15 后有过口径变更（角色会话相关注入已退场）。
