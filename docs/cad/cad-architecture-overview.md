# CAD 自动化架构概览 — AI 驱动设计三支柱

> **⚠️ 本文已过时**
>
> 本文描述的是 CAD 专属 `commandstack/` 方案，已被通用 `mcpstack/` 中间件取代。
> 当前 Seelex 的 MCP 追溯架构见 [`../arch/design-decisions-mcp-storage.md`](../arch/design-decisions-mcp-storage.md)（设计决策）和 [`../arch/mcp-call-chain-flowchart.md`](../arch/mcp-call-chain-flowchart.md)（调用链路）。
> FreeCAD 不再自研 MCP Server，而是按 `mcp_servers` 配置（与 WebSearch 同一生态位）选用现成实现。

> 版本: v1.0  
> 创建日期: 2025-07-18  
> 状态: ❌ 已废弃（见上方替代文档）  
> 关联文档: `cad-command-stack.md`, `cad-mcp-bridge.md`, `cad-freecad-executor.md`

---

## 目录

1. [核心理念](#1-核心理念)
2. [三支柱架构图](#2-三支柱架构图)
3. [数据流](#3-数据流)
4. [与现有 Seelex 基础设施的映射](#4-与现有-seelex-基础设施的映射)
5. [模块依赖拓扑](#5-模块依赖拓扑)
6. [关键设计决策](#6-关键设计决策)
7. [路线图与优先级](#7-路线图与优先级)

---

## 1. 核心理念

将 **Seelex AI** 改造为 AI 驱动的 CAD 设计自动化系统，遵循三条支柱：

| 支柱 | 定位 | 职责 |
|------|------|------|
| **FreeCAD 执行底座** | 物理执行层 | 运行 headless FreeCAD 服务，通过 Python API 执行具体 CAD 操作（草图、拉伸、开孔等） |
| **MCP 连接桥** | 协议通信层 | 基于 Model Context Protocol，在 Seelex（Go）与 FreeCAD 服务（Python）之间建立双向 JSON-RPC 通信 |
| **JSON 命令栈** | 数据历史层 | 不可变、可序列化的 CAD 操作历史，作为设计的单一事实来源（SSOT），支撑 undo/redo 和设计回溯 |

### 设计原则

- **分离关注**：三支柱各自独立可测试，通过接口契约通信
- **SSOT**：JSON 命令栈是唯一权威的设计历史，FreeCAD 文件仅为渲染产物
- **可追溯**：每条命令记录 AI 决策理由，形成完整的设计思维链
- **可恢复**：从命令栈可完全重放设计过程，不依赖 FreeCAD 的 `.FCStd` 格式

---

## 2. 三支柱架构图

```
  ┌─────────────────────────────────────────────────────────────────┐
  │                    Seelex AI (Go)                               │
  │                                                                 │
  │  ┌─────────────────────────────────────────────────────────┐    │
  │  │                  Seele Engine                            │    │
  │  │   Agent  →  LLM  →  Tool Calls  →  WorkPlan             │    │
  │  └────────────────────┬────────────────────────────────────┘    │
  │                       │                                         │
  │           ┌───────────┼───────────┐                             │
  │           ▼           ▼           ▼                             │
  │  ┌────────────┐ ┌──────────┐ ┌──────────┐                     │
  │  │  MCP 工具  │ │ 内置工具  │ │ 工作流工具 │                     │
  │  │ (CAD相关)  │ │          │ │          │                     │
  │  └─────┬──────┘ └──────────┘ └──────────┘                     │
  │        │                                                       │
  └────────┼───────────────────────────────────────────────────────┘
           │ MCP (stdio / JSON-RPC)
           ▼
  ┌─────────────────────────────────────────────────────────────────┐
  │              MCP Server (Python)                                │
  │                                                                 │
  │  ┌─────────────────────────────────────────────────────────┐    │
  │  │            freecad_mcp_server.py                        │    │
  │  │  sketch_rectangle  │  pad_extrude  │  pocket_circular   │    │
  │  │  fillet  │  chamfer  │  export_stl  │  export_step       │    │
  │  └──────────────────────────┬──────────────────────────────┘    │
  │                             │ FreeCAD Python API                │
  │                             ▼                                   │
  │  ┌─────────────────────────────────────────────────────────┐    │
  │  │              FreeCAD (headless)                         │    │
  │  │  App │  Part │  PartDesign │  Sketcher │  Mesh          │    │
  │  └─────────────────────────────────────────────────────────┘    │
  └─────────────────────────────────────────────────────────────────┘

  ┌─────────────────────────────────────────────────────────────────┐
  │               JSON 命令栈 (commandstack/)                      │
  │                                                                 │
  │  [                                      ]                        │
  │  { "seq":1, "op":"sketch_rectangle",    }                        │
  │  { "seq":2, "op":"pad_extrude",         }  ← CurrentIdx        │
  │  { "seq":3, "op":"sketch_circle",       }                        │
  │  { "seq":4, "op":"pocket_circular",     }                        │
  │  [                                      ]                        │
  │                                                                 │
  │  • Append-only 不可变历史                                       │
  │  • JSON 序列化 ↔ 文件持久化                                      │
  │  • Undo/Redo 通过 CurrentIdx 指针                                │
  │  • 嵌入 ContextSnapshot 实现上下文承袭                           │
  └─────────────────────────────────────────────────────────────────┘
```

---

## 3. 数据流

### 3.1 正向设计流（用户 → 设计结果）

```
用户输入                                FreeCAD 渲染
    │                                       ▲
    ▼                                       │
┌──────────┐  MCP 工具调用    ┌──────────────────────┐
│ Seelex   │ ───────────────► │ freecad_mcp_server   │
│ (LLM)    │    JSON-RPC      │  (执行 CAD 操作)     │
│          │ ◄─────────────── │                      │
│ 每一步   │   结果 + 状态     └──────────┬───────────┘
│ 写入     │                              │
│ 命令栈   │                              │ FreeCAD Python API
└────┬─────┘                              │
     │                                    ▼
     ▼                            ┌────────────────┐
┌──────────┐                     │ FreeCAD        │
│Command   │  append             │ Document (.FCStd)
│Stack     │ ◄────────────────── │                │
│(JSON)    │  命令记录            └────────────────┘
└──────────┘
```

### 3.2 逆向流（加载 → 恢复 → 修改）

```
启动时 ← 加载命令栈 JSON
   │
   ├─→ 场景 A：从头重放
   │     遍历 Commands[0..CurrentIdx]
   │     每条通过 MCP 发送到 FreeCAD 执行
   │     恢复设计状态
   │
   └─→ 场景 B：增量恢复
         从上次保存的位置继续
         只发送 Commands[savedIdx+1..CurrentIdx]
```

### 3.3 设计回溯流（LLM 推理时阅读上下文）

```
LLM 需要理解当前设计状态
        │
        ▼
Seele Engine 调用 provider.CommandStackProvider
        │
        ▼
CommandStackProvider.Export()
  → 从 CommandStack 提取：
    • 最近 N 条命令（结构化）
    • DesignGoal（目标）
    • Constraints（约束）
    • 当前参数摘要
  → 格式化为 ContextSnapshot
  → 注入 system prompt
```

---

## 4. 与现有 Seelex 基础设施的映射

### 4.1 现有代码可以直接复用的部分

| 现有包 | 复用方式 | 说明 |
|--------|----------|------|
| `snapshot.ContextSnapshot` | 嵌入 `CommandStack.Metadata` | 直接复用 Goal、Decisions、Constraints 等字段，作为命令栈的元数据 |
| `provider.Provider` 接口 | 实现 `CommandStackProvider` | 让命令栈成为上下文来源之一，与 EngineProvider、TraceProvider 并列 |
| `provider.Compactable` 接口 | 长设计历史的 token 压缩 | 命令数超过阈值时自动摘要（如"共 47 条操作，最近 5 条：..."） |
| `provider.Mergable` 接口 | 多阶段设计的上下文合并 | 子设计（如"底座"）完成后合并到父设计 |
| `compactor.Compactor` | 上下文注入前的 Token 预算控制 | 避免超长命令栈撑爆 Token 窗口 |
| `merger.Merger` | 设计阶段合并 | 与 Mergable 配合，实现设计拆分与合并 |
| `seelexctx` 包的 Export/Import | 跨会话的设计上下文传递 | 一个会话的设计状态可导出到另一个会话继续 |
| `session.Manager` | 多会话管理 | 每个 CAD 设计项目对应一个 session |

### 4.2 需要新建的模块

| 新包 | 职责 | 代码行数估算 |
|------|------|:------------:|
| `commandstack/` | JSON 命令栈类型 + 序列化 + undo/redo + 验证 | ~350 行 |
| `mcp/` | MCP 协议客户端（stdio transport）+ 工具注册 | ~400 行 |
| `freecad/` | FreeCAD 工具定义（schema）+ 参数验证 | ~300 行 |
| `freecad/server/` | Python MCP Server（独立进程） | ~500 行 Python |
| `commandstack/provider.go` | 实现 Provider 接口，从命令栈导出上下文 | ~100 行 |

### 4.3 不需要修改的现有模块

| 现有模块 | 原因 |
|----------|------|
| `tui/` | TUI 层不需要修改，新工具通过 ToolCalls 调用 |
| `session/` | 会话管理已够用，新模块注册到现有会话即可 |
| `skill/` | 如需 CAD 提示词技能，可注册为新 skill |
| `main.go` | 仅需在装配阶段增加 MCP Client 和 CommandStack 的初始化 |

---

## 5. 模块依赖拓扑

```
main.go
  ├── session/        (无需修改)
  ├── skill/          (可选新增 CAD skill)
  ├── commandstack/   (⇄ snapshot, ⇄ provider)
  │     └── snapshot/ (嵌入 Metadata)
  ├── mcp/
  │     └── (std: os/exec, io: bufio)
  └── freecad/
        └── (仅 schema/ 定义)

外部依赖：
  mcp/ → github.com/mark3labs/mcp-go (已在 go.mod)
  freecad/ → Python 3 + FreeCAD 库（外部进程，非 Go 包）
```

---

## 6. 关键设计决策

### 决策 1：为什么不把命令栈写在 FreeCAD 的 `.FCStd` 里？

| 方案 | 评价 |
|------|------|
| ✅ **JSON 命令栈独立存储** | 纯文本、可 diff、可版本控制、AI 可直接读写 |
| ❌ 写入 `.FCStd` 内部 | 二进制格式、不可 diff、AI 无法直接解析 |

### 决策 2：为什么用 MCP 而不是直接调用 FreeCAD Python API？

| 方案 | 评价 |
|------|------|
| ✅ **MCP 协议** | 标准协议、松耦合、Go 端无需 FreeCAD 库、可轻松替换为其他 CAD |
| ❌ 直接 cgo/cpython | 紧耦合、FreeCAD C++ API 复杂、跨平台问题多 |
| ❌ gRPC | 多一层依赖、FreeCAD 端需额外 gRPC 库 |

### 决策 3：undo/redo 的实现方式？

| 方案 | 评价 |
|------|------|
| ✅ **命令指针模式** | `CurrentIdx` 指针在数组中移动，undo=指针前移，redo=指针后移 |
| ❌ 删除命令 | 破坏不可变性，不可追溯 |
| ❌ FreeCAD 内置 undo | 依赖 FreeCAD 状态，跨会话不可恢复 |

### 决策 4：命令栈与 ContextSnapshot 的关系？

| 方案 | 评价 |
|------|------|
| ✅ **嵌入复用** | `CommandStack.Metadata` 复用 `snapshot.ContextSnapshot` 的部分字段 |
| ❌ 独立定义重复字段 | 两地维护双份 Goal/Constraints，容易不一致 |

---

## 7. 路线图与优先级

### Phase 1：基础设施（优先级：最高）

```
□ commandstack/     — 核心数据结构 + JSON 序列化 + 文件持久化
□ commandstack/undo — undo/redo 逻辑（指针操作）
□ commandstack/test — 单元测试（序列化反序列化、边界条件）
```

### Phase 2：通信层（优先级：高）

```
□ mcp/client.go     — MCP 客户端（stdio transport）
□ mcp/register.go   — 工具注册（注册到 Seele ToolHolder）
□ freecad/ops.go    — CAD 操作类型定义 + JSON Schema
□ freecad/server/   — Python MCP Server 骨架（接收 → 执行 → 返回）
```

### Phase 3：集成与验证（优先级：中）

```
□ main.go 装配       — 初始化 MCP Client + CommandStack
□ provider/commandstack.go — 实现 Provider 接口
□ 端到端流程验证       — "用户一句话 → 完整 CAD 操作序列"
```

### Phase 4：增强功能（优先级：低）

```
□ 命令栈 GUI 浏览器   — TUI 中查看/回滚设计历史
□ 多设计分支          — 基于命令栈的分支/合并（类似 Git）
□ AI 自动修复         — 失败命令的自动重试/修正
□ 参数优化循环        — AI 分析设计结果 → 调整参数 → 重新生成
```

---

## 附录 A：概念验证示例

### 用户输入到命令栈的完整路径

```json
// 用户: "做一个长100mm宽50mm高30mm的盒子，顶部挖一个直径20mm的圆孔"

// LLM 拆解为一系列 CAD 操作，分步调用 MCP 工具：

// Step 1: 创建草图
{
  "seq": 1,
  "op": "sketch_rectangle",
  "params": {"plane": "XY", "length": 100, "width": 50},
  "timestamp": "2025-07-18T10:00:01Z",
  "ai_reasoning": "在 XY 平面上创建 100×50mm 的矩形作为盒子底面"
}

// Step 2: 拉伸成体
{
  "seq": 2,
  "op": "pad_extrude",
  "params": {"sketch_seq": 1, "height": 30, "direction": "Z"},
  "timestamp": "2025-07-18T10:00:03Z",
  "ai_reasoning": "将底面拉伸 30mm 形成盒体"
}

// Step 3: 在顶面创建圆形草图
{
  "seq": 3,
  "op": "sketch_circle",
  "params": {"face": "top", "center_x": 50, "center_y": 25, "radius": 10},
  "timestamp": "2025-07-18T10:00:05Z",
  "ai_reasoning": "在顶面中心创建直径 20mm 的圆作为挖孔位置"
}

// Step 4: 挖孔
{
  "seq": 4,
  "op": "pocket_circular",
  "params": {"sketch_seq": 3, "depth": 30, "through": true},
  "timestamp": "2025-07-18T10:00:07Z",
  "ai_reasoning": "从顶面打穿 30mm 深通孔，形成盒体开口"
}
```

---

## 附录 B：术语表

| 术语 | 解释 |
|------|------|
| MCP | Model Context Protocol，模型上下文协议，Anthropic 开源的 LLM-工具通信标准 |
| SSOT | Single Source of Truth，单一事实来源 |
| Command Stack | 不可变的命令序列，作为设计的权威历史记录 |
| Headless FreeCAD | 以服务模式运行的 FreeCAD，无 GUI |
| Inverse Command | 可撤销一条命令效果的逆操作描述 |
| CurrentIdx | 命令栈中当前状态的指针位置 |
