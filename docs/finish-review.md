# Seelex 机械设计方向与代码最终审查

> 审查日期：2026-07-17
> 审查提交：`54e3e0d`
> 范围：现有 Go 代码、测试、CI，以及 `docs/cad-*.md` 四份 CAD 设计文档
> 说明：本次只做审查，没有修改业务代码。

## 结论先行

Seelex 走“机械设计插件 + FreeCAD MCP + JSON 操作历史”的方向是可行的，但当前不能直接按现有 CAD 文档开工。

主要原因有三类：

1. 当前仓库无法全量构建：`go.mod` 固定的 `Seele v0.0.1` 不包含源码正在导入的 `permission` 包。
2. CAD 目前只有约 2300 行设计文档，没有 `commandstack/`、`mcp/`、`freecad/` 或 Python Server 实现，CAD 测试覆盖率实际为 0%。
3. 现有文档骨架存在会死锁、非幂等、错误 JSON Schema、序号不一致、FreeCAD 线程不安全等阻断问题。

建议先完成 P0 平台加固，再做 P1 FreeCAD 最小闭环。机械工程图、装配、BOM、公差等能力应放在后续阶段，不应和第一版 MCP 同时铺开。

## 当前能力盘点

| 能力 | 当前状态 | 结论 |
|---|---|---|
| LLM/工具调度 | 由 Seele 提供 | 可复用，但依赖版本当前错配 |
| TUI 工具调用展示 | 已有 | 可复用，但全局通道存在竞争且无法取消长任务 |
| 权限门控 | 已有 | 可扩展到 CAD 写文件/启动进程，但依赖包当前无法解析 |
| Skill | 只有平铺 Markdown Loader | 不是动态插件系统，且 Create/Delete 有死锁与路径穿越风险 |
| Plugin | `main.go` 中硬编码的工具可见性过滤器 | 不能直接承载“机械设计插件目录 + MCP 配置 + 生命周期” |
| Session/上下文 | 已有基础类型 | 可作为设计会话元数据，但不等于 CAD 项目存储 |
| JSON 命令栈 | 仅文档 | 未实现、未测试 |
| MCP Client | 仅文档 | 未实现，文档骨架不可直接使用 |
| FreeCAD Server | 仅文档 | 未实现，文档示例离可靠机械建模仍有明显距离 |
| 工程图/装配/BOM/GD&T | 无 | 机械设计产品化的主要缺口 |

## JSON 驱动 CAD 历史是否可行

### 可行部分

- 每个 CAD 意图使用可版本化 JSON 命令表达，便于存储、重放、审计和让 Agent 修改参数。
- 线性 undo/redo 使用“命令数组 + 当前 head 指针”比把两个运行时栈直接持久化更适合作为文件格式。
- `.FCStd` 可以作为加速恢复的检查点，JSON 历史作为逻辑设计历史。

### 必须调整的部分

现有文档同时宣称“命令不可删除、仅追加”，又在历史中间 Push 时丢弃未来命令，这两个规则互相冲突。若未来需要保留失败尝试，不能删除未来段，应把历史建模成 revision DAG：

```text
CommandIntent (不可变) -> ExecutionResult (不可变事件)
           \-> parent_revision_id

Branch -> head_revision_id
Tag    -> revision_id
```

第一版可以只暴露线性 UI，但底层至少应保留：

- `schema_version`、`op_version`、`engine_version`、FreeCAD/Workbench 版本；
- 稳定的 `entity_id` 和依赖关系，避免用数组序号或 `Edge3` 作为长期引用；
- `operation_id`/幂等键，防止 MCP 超时重试创建重复特征；
- `transaction_id`，把一次用户意图拆出的多个原子操作作为一个 undo 单元；
- 前置条件、后置结果、错误和几何校验摘要；
- 外部输入文件的内容哈希；
- checkpoint 与其对应的 revision/head/hash；
- 数据迁移器，保证旧命令能在新版本读取。

`AIReasoning` 不应保存模型的原始思维链。建议只存简洁的 `rationale`、用户需求引用、约束和可公开审计的决策摘要。

### Undo/Redo 推荐方案

- 权威恢复：移动 branch head，并从最近 checkpoint 重放到目标 revision。
- 快速交互：可借助 FreeCAD transaction 或受控的局部撤销，但完成后必须与命令历史和几何校验对账。
- 新操作发生在旧 head：创建新分支，原未来历史保留；不要截断数组。
- 不把 `Inverse` 当作普适真相。参数修改、布尔运算、拓扑变化和装配约束通常无法用简单“删除对象”可靠逆转。

## 代码审查发现

### 严重问题

#### 1. 当前提交无法全量构建

- `go.mod:6` 固定 `github.com/RedHuang-0622/Seele v0.0.1`。
- `main.go:19` 导入 `github.com/RedHuang-0622/Seele/agent/core/tool/permission`。
- 已下载并检查的 `Seele v0.0.1` 中没有该目录。

`go build ./...`、`go vet ./...`、`go test ./...` 均在 package loading 阶段失败。应先发布包含当前 API 的 Seele tag，并让 Seelex 固定到该版本；不建议重新依赖未提交的本地 `replace`。

#### 2. Skill Create/Delete 会自锁死锁，并允许路径逃逸

- `skill/skill.go:109-114`：`Create` 持有写锁后调用 `PrimaryDir()`，后者再次获取同一 `RWMutex` 的读锁。Go 的 `RWMutex` 不可重入，会永久阻塞。
- `skill/skill.go:138-143`：`Delete` 有同样问题。
- `skill/skill.go:120,143`：未经验证的 name 直接进入 `filepath.Join`，`../x`、绝对路径或平台分隔符可能逃出 skills 目录。

虽然当前 UI 没有接通 Create/Delete，但未来动态机械插件/Skill 管理会直接踩到该问题。

#### 3. 审批和工具事件桥接存在数据竞争及请求覆盖

- `tui/approve/approve.go:38-53` 使用无锁全局单槽 `pendingRequest`。两个工具同时审批时，后写请求覆盖前一个，前一个 goroutine 可永久等待。
- `tui/stream.go:23,27-30,215-248` 使用无锁全局 `streamEventCh`。并发会话或流任务会覆盖通道，race 模式下应视为高风险。
- `tui/stream.go:39` 使用 `context.Background()`；`tui/tui.go:195-199,399` 的 Ctrl+C 退出不会取消 Engine、MCP 或 FreeCAD 长任务。

机械 CAD 操作通常耗时长，且可能触发审批、进度与重试，因此必须改成每个 Model/Session 独立的事件总线和可取消 context。

#### 4. MCP 文档骨架包含确定性死锁和协议缺口

- `docs/cad-mcp-bridge.md:305-345`：`Start` 持有 `c.mu` 再调用 `initialize`；`initialize -> sendRequest` 在 372 行再次锁 `c.mu`，必然自锁。
- `docs/cad-mcp-bridge.md:496-524`：`Close` 持锁等待进程，响应读取协程退出时也需要同一锁，存在关闭死锁。
- 并发请求写 stdin 没有独立 write mutex，JSON 行可能交错。
- stdout 关闭时直接 close 等待通道；接收方可能拿到 nil response 且 nil error。
- 初始化成功后没有发送 `notifications/initialized`。
- 只接受 string ID，忽略通知、进度、日志、工具列表变化和取消通知。
- 协议固定为 `2024-11-05`，缺少协商和兼容策略。
- 文档声称 `mcp-go v0.54.0` 的 stdio 不完善，但该依赖已经包含 `client/transport/stdio.go`；应先评估复用库实现，避免重复实现协议。

#### 5. FreeCAD 文档骨架无法满足其“幂等恢复”目标

- `docs/cad-freecad-executor.md:155-162` 的 `mustSchema` 只是序列化零值 struct，得到的不是 JSON Schema。
- `feature_count` 从 0 开始，首个草图为 `Sketch_0` 并返回 id 0；Go 验证又要求 `SketchSeq > 0`，序号契约冲突。
- 重试会继续创建 Sketch/Pad，没有 operation id 去重，因此不幂等。
- `fillet.edge_ids` 依赖易变化的拓扑序号，模型重算后可能指向不同边。
- `threading.Thread + join(timeout)` 不能终止 FreeCAD 操作；超时后后台线程仍可能继续修改文档。FreeCAD API 也不应被任意后台线程并发调用。
- `safe_recompute` 捕获失败后直接 `initialize()`，会新建文档并丢失当前设计，同时把错误降级成警告。
- handler 异常前可能已经部分修改文档，没有事务回滚。

应使用单线程串行执行器；超时需要在进程级隔离和终止；每个操作用 FreeCAD transaction 包裹，失败 abort，并在成功后执行 recompute、文档错误检查和几何有效性检查。

### 警告问题

- 36/36 个 Go 文件都不符合 `gofmt`，CI 没有格式门禁。
- `main.go:76` 和测试中的 `Pool.All()[0]` 没有空账号校验，可 panic。
- `main.go:330-337` 静默忽略权限文件读取和 YAML 解析错误，配置错误可能被掩盖。
- `snapshot.Truncate` 使用字节切片，可能截断 UTF-8；当 `maxLen < 3` 时可能产生负切片下标。
- 集成测试依赖本地 LLM 配置并在缺失时整体 skip，不是可重复的 CI 集成测试。
- `docs/cad-architecture-overview.md` 认为 TUI、Session、Skill 无需修改，与实际并发、插件和项目生命周期需求不符。

## 机械设计方向还缺什么

### P0：平台稳定性与插件底座

1. 修复 Seele 版本/API 错配，恢复三平台 build/vet/test。
2. 把硬编码 mode 改成可发现的 plugin manifest：名称、工具组、MCP servers、skills、permissions、artifact types、生命周期。
3. 建立 MCP connection manager：启动、健康检查、协议协商、取消、重连、进度、日志、资源/图片结果。
4. 为每个 session/project 隔离 context、审批队列、工具事件和 FreeCAD 进程。
5. 统一 artifact/workspace 安全策略，限制导出路径、子进程和宏执行。
6. 建立 gofmt、覆盖率阈值、race、依赖漏洞扫描和 Python 测试门禁。

### P1：可靠的 FreeCAD 零件建模 MVP

1. 版本化 CAD IR/命令事件、branch head、transaction、idempotency、checkpoint。
2. 稳定对象 ID 与引用解析，避免 seq 和裸 edge index。
3. 草图几何约束、尺寸约束、完全约束检查和 solver 状态。
4. Pad/Pocket/Revolve/Boolean/Fillet/Chamfer/Pattern/Datum 的严格 schema 与边界校验。
5. 单线程 FreeCAD 执行器、事务回滚、recompute 检查、BRep/solid 有效性校验。
6. FCStd/STEP/STL 导入导出、预览图、包围盒、体积、质量、重心等结构化结果。
7. 端到端黄金模型：命令 JSON -> FCStd/STEP -> 几何断言 -> 重放 hash/关键属性一致。

### P2：机械工程图

1. TechDraw 页面、模板、标题栏和修订栏。
2. 主/俯/侧视图、轴测、剖视、局部放大、隐藏线和中心线。
3. 尺寸、公差、配合、表面粗糙度、形位公差（GD&T）、焊接/螺纹符号。
4. 尺寸关联性与视图更新，避免模型修改后工程图失联。
5. PDF/SVG/DXF 导出及图纸规则检查。

### P3：装配与工程数据

1. Part/Assembly 层级、装配约束、自由度检查、干涉与间隙检查。
2. 标准件库、材料库、紧固件/轴承/齿轮等参数化组件。
3. BOM、零件编号、属性、版本和替代件管理。
4. 配置/变体、参数表、表达式和设计表。
5. 质量属性、材料密度、成本/重量估算。

### P4：分析与制造

1. 基础 DFM 规则：最小壁厚、孔边距、圆角、拔模、刀具可达性。
2. FEM/载荷/边界条件的受控工作流，结果必须明确假设与置信度。
3. Sheet Metal、焊件、CAM/路径等专业 Workbench 适配。

## 推荐的机械插件边界

建议“机械设计 plugin”拥有领域能力，但不要把所有状态塞进 MCP：

| 层 | 建议职责 |
|---|---|
| Seelex Core | 动态 plugin registry、权限、session、artifact、事件总线、MCP connection manager |
| mechanical-design plugin | FreeCAD MCP 声明、CAD tools/skills、领域 schema、验证器、工程图/装配工作流 |
| CAD runtime | 命令事件图、branch/tag、transaction、checkpoint、重放与验证 |
| FreeCAD MCP Server | 单线程执行 FreeCAD API、事务、几何检查、文件/预览产物 |

切换插件只应改变工具可见性和工作流，不应销毁项目状态或 FreeCAD 进程。项目关闭时再由生命周期管理器保存并退出。

## 五轴评分

| 维度 | 状态 | 评分 | 备注 |
|---|:---:|:---:|---|
| 正确性 | 🚫 | D | 全量构建失败；Skill 死锁；CAD 文档含确定性死锁与契约错误 |
| 可读性 | ⚠️ | C | 文档丰富，但 36/36 Go 文件未 gofmt，部分代码过度压行 |
| 架构 | ⚠️ | C | 上下文模块有分层基础；插件、CAD runtime、项目生命周期尚未形成 |
| 安全性 | 🚫 | D | Skill 路径穿越；CAD 导出路径/子进程/宏权限边界未设计完整 |
| 性能与并发 | 🚫 | D | 全局无锁桥接、无取消；FreeCAD 超时线程方案不安全 |
| 测试 | 🚫 | D | 可编译非根包总体 13.5%；核心交互和全部 CAD 为 0% |

## 最终判断

- [ ] 通过，可直接进入 FreeCAD MCP 实现
- [ ] 有条件通过
- [x] 不通过：先完成 P0，再按修订后的事件历史和 MCP/FreeCAD 契约做 P1

最值得保留的是“JSON 可回溯设计历史 + MCP 隔离 CAD 后端”的总体方向；最需要推翻的是当前文档中的自实现 MCP 骨架、简单 inverse undo、seq/edge 引用和线程超时方案。
