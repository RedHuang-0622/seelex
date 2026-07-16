# JSON 命令栈 — commandstack 包详细设计

> 版本: v1.0  
> 创建日期: 2025-07-18  
> 状态: 方案草案  
> 关联文档: `cad-architecture-overview.md`, `cad-mcp-bridge.md`, `cad-freecad-executor.md`

---

## 目录

1. [设计目标](#1-设计目标)
2. [数据结构](#2-数据结构)
3. [核心接口](#3-核心接口)
4. [序列化规范](#4-序列化规范)
5. [Undo/Redo 机制](#5-undoredo-机制)
6. [文件持久化](#6-文件持久化)
7. [与 ContextSnapshot 的集成](#7-与-contextsnapshot-的集成)
8. [Provider 实现](#8-provider-实现)
9. [线程安全](#9-线程安全)
10. [完整代码骨架](#10-完整代码骨架)

---

## 1. 设计目标

- **不可变性**：已写入的命令不可删除或修改，仅追加
- **可序列化**：完整命令栈可序列化为 JSON 文件，也可反序列化恢复
- **可重放**：给定命令序列，可完全恢复设计状态
- **可追溯**：每条命令记录 AI 决策理由
- **可撤销/重做**：通过指针移动实现，不破坏历史
- **跨会话**：命令栈可持久化到文件，在不同会话间传递

### 非目标

- 不承担实时协作（多用户同时编辑）——未来可通过分支扩展
- 不承担 FreeCAD 具体操作逻辑——那是 `mcp/` 和 `freecad/` 的职责
- 不承担版本管理（Git-like 分支合并）——Phase 4 可选

---

## 2. 数据结构

### 2.1 CommandStack — 顶层容器

```go
package commandstack

import (
    "encoding/json"
    "fmt"
    "sync"
    "time"

    "github.com/RedHuang-0622/seelex/snapshot"
)

// CommandStack 是 CAD 操作的线性历史，JSON 可序列化，SSOT。
// 所有字段首字母大写以确保 JSON 导出。
type CommandStack struct {
    mu sync.RWMutex `json:"-"` // 序列化时忽略

    SessionID   string          `json:"session_id"`             // 会话 ID
    CreatedAt   time.Time       `json:"created_at"`             // 创建时间
    UpdatedAt   time.Time       `json:"updated_at"`             // 最后修改时间
    Commands    []Command       `json:"commands"`               // 完整命令历史（不可变，仅追加）
    CurrentIdx  int             `json:"current_idx"`            // 当前状态指针（-1 表示空栈）
    Metadata    CommandMetadata `json:"metadata"`               // 设计元信息
    Tags        map[string]string `json:"tags,omitempty"`       // 标签（如 version="v1", author="user"）
}

// CommandMetadata 设计元信息 —— 复用 snapshot.ContextSnapshot 的设计
type CommandMetadata struct {
    DesignGoal     string            `json:"design_goal,omitempty"`     // 设计目标
    Decisions      []snapshot.Decision `json:"decisions,omitempty"`    // 关键决策记录
    Findings       []string          `json:"findings,omitempty"`       // 重要发现
    Progress       string            `json:"progress,omitempty"`       // 进度描述
    Constraints    []string          `json:"constraints,omitempty"`    // 约束条件
    Units          string            `json:"units"`                     // 单位（mm/inch）
    FreeCADVersion string            `json:"freecad_version,omitempty"` // FreeCAD 版本
    Author         string            `json:"author,omitempty"`          // 设计者
}
```

### 2.2 Command — 单条操作记录

```go
// Command 单条 CAD 操作记录，不可变，写入后不修改。
type Command struct {
    ID          string          `json:"id"`                    // UUID v4，全局唯一
    Seq         int             `json:"seq"`                   // 序号（从 1 开始递增）
    Timestamp   time.Time       `json:"timestamp"`             // 命令创建时间
    Op          string          `json:"op"`                    // 操作类型（见下方 Op 常量）
    Params      json.RawMessage `json:"params"`                // 操作参数 JSON
    Inverse     json.RawMessage `json:"inverse,omitempty"`     // 逆操作参数（用于 undo，可选）
    AIReasoning string          `json:"ai_reasoning,omitempty"` // AI 决策理由
    Status      CommandStatus   `json:"status"`                // 执行状态
    ErrorMsg    string          `json:"error_msg,omitempty"`   // 执行失败时的错误信息
    TokenCount  int             `json:"token_count,omitempty"` // 该命令的 token 估算（用于 context 预算）
}

// CommandStatus 命令执行状态
type CommandStatus int

const (
    StatusPending   CommandStatus = iota // 待执行
    StatusExecuted                       // 执行成功
    StatusFailed                         // 执行失败
    StatusUndone                         // 已被撤销
)
```

### 2.3 操作类型常量

```go
// 操作类型常量 —— 对应 FreeCAD Python API 的操作名
const (
    // 草图操作
    OpSketchRectangle  = "sketch_rectangle"
    OpSketchCircle     = "sketch_circle"
    OpSketchPolygon    = "sketch_polygon"
    OpSketchLine       = "sketch_line"
    OpSketchArc        = "sketch_arc"
    OpSketchConstraint = "sketch_constraint"

    // 3D 特征
    OpPadExtrude       = "pad_extrude"
    OpPocketCircular   = "pocket_circular"
    OpPocketRectangular = "pocket_rectangular"
    OpRevolution       = "revolution"
    OpGroove           = "groove"

    // 修饰特征
    OpFillet           = "fillet"
    OpChamfer          = "chamfer"
    OpMirror           = "mirror"
    OpLinearPattern    = "linear_pattern"
    OpCircularPattern  = "circular_pattern"

    // 参考几何
    OpDatumPlane       = "datum_plane"
    OpDatumLine        = "datum_line"

    // 文件操作
    OpExportSTL        = "export_stl"
    OpExportSTEP       = "export_step"
    OpSaveFCStd        = "save_fcstd"

    // 系统操作
    OpUndo             = "system_undo"     // 记录 undo 操作（可选）
    OpRedo             = "system_redo"     // 记录 redo 操作（可选）
    OpClear            = "system_clear"    // 清空设计
)
```

---

## 3. 核心接口

### 3.1 栈操作

```go
// Stack 定义命令栈的核心操作接口。
type Stack interface {
    // Push 追加一条命令到栈顶（currentIdx 之后）。
    // 如果当前位置不在栈顶，则丢弃 currentIdx 之后的命令。
    Push(cmd Command) error

    // Undo 将 CurrentIdx 前移一位，返回被撤消的命令。
    // 如果已在栈底，返回 ErrStackBottom。
    Undo() (*Command, error)

    // Redo 将 CurrentIdx 后移一位，返回被重做的命令。
    // 如果已在栈顶，返回 ErrStackTop。
    Redo() (*Command, error)

    // Current 返回当前状态的最后一条命令（即 Commands[CurrentIdx]）。
    Current() (*Command, error)

    // Peek 向前/向后查看 N 条命令（不移动指针）。
    Peek(offset int) (*Command, error)

    // Length 返回命令总数（包括已 undo 的）。
    Length() int

    // Count 返回当前状态的有效命令数（CurrentIdx + 1）。
    Count() int
}

var (
    ErrStackBottom = fmt.Errorf("commandstack: already at bottom")
    ErrStackTop    = fmt.Errorf("commandstack: already at top")
    ErrEmptyStack  = fmt.Errorf("commandstack: stack is empty")
    ErrInvalidSeq  = fmt.Errorf("commandstack: invalid sequence number")
)
```

### 3.2 序列化

```go
// Marshal 序列化命令栈为 JSON（线程安全）。
func (cs *CommandStack) Marshal() ([]byte, error)

// Unmarshal 从 JSON 恢复命令栈（线程安全）。
func (cs *CommandStack) Unmarshal(data []byte) error

// Save 保存到文件（原子写入：先写 tmp 再 rename）。
func (cs *CommandStack) Save(path string) error

// Load 从文件加载。
func (cs *CommandStack) Load(path string) error
```

### 3.3 快照

```go
// Snapshot 返回当前状态的深度拷贝（线程安全）。
// 用于传递到 provider 或其他协程。
func (cs *CommandStack) Snapshot() (*CommandStack, error)
```

---

## 4. 序列化规范

### 4.1 JSON 表示

```json
{
  "session_id": "cad-session-20250718-001",
  "created_at": "2025-07-18T10:00:00Z",
  "updated_at": "2025-07-18T10:05:00Z",
  "current_idx": 2,
  "metadata": {
    "design_goal": "设计一个 100×50×30mm 的塑料外壳",
    "constraints": ["壁厚 ≥ 2mm", "圆角半径 ≥ 1mm"],
    "units": "mm"
  },
  "tags": {
    "author": "user",
    "project": "enclosure-v1"
  },
  "commands": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "seq": 1,
      "timestamp": "2025-07-18T10:00:01Z",
      "op": "sketch_rectangle",
      "params": {"plane": "XY", "length": 100, "width": 50},
      "inverse": {"operation": "delete_sketch", "sketch_id": 1},
      "ai_reasoning": "在 XY 平面上创建 100×50mm 的矩形作为外壳底面",
      "status": 1,
      "token_count": 15
    },
    {
      "id": "550e8400-e29b-41d4-a716-446655440001",
      "seq": 2,
      "timestamp": "2025-07-18T10:00:03Z",
      "op": "pad_extrude",
      "params": {"sketch_seq": 1, "height": 30, "direction": "Z"},
      "inverse": {"operation": "delete_feature", "feature_id": 1},
      "ai_reasoning": "将底面拉伸 30mm 形成盒体",
      "status": 1,
      "token_count": 10
    },
    {
      "id": "550e8400-e29b-41d4-a716-446655440002",
      "seq": 3,
      "timestamp": "2025-07-18T10:00:05Z",
      "op": "sketch_circle",
      "params": {"face": "top", "center_x": 50, "center_y": 25, "radius": 10},
      "inverse": {"operation": "delete_sketch", "sketch_id": 2},
      "ai_reasoning": "在顶面中心创建直径 20mm 的圆孔位置",
      "status": 1,
      "token_count": 12
    }
  ]
}
```

### 4.2 文件命名约定

```
.selelex/cad/{session_id}.cmdstack.json
```

- 存储在 `.seelex/cad/` 目录下
- 文件名 = `{session_id}.cmdstack.json`
- 支持通过 `SEELEX_CAD_DIR` 环境变量覆盖路径

---

## 5. Undo/Redo 机制

### 5.1 指针模型

```
Undo ←─ CurrentIdx ─→ Redo
          │
          ▼
命令数组 [cmd1, cmd2, cmd3, cmd4, cmd5]
         ▲───────────▲────────────▲
         │           │            │
      已执行       当前状态     可 redo
                   （已应用）  （已 undo）
```

### 5.2 规则

| 操作 | CurrentIdx 变化 | 效果 |
|------|:--------------:|------|
| Push | `→ len-1` | 追加到栈顶，丢弃之后所有命令 |
| Undo | `−1` | 指针前移，返回被 undo 的命令 |
| Redo | `+1` | 指针后移，返回被 redo 的命令 |
| Push 后 Undo | 不特殊 | 正常前移，被丢弃的命令不可恢复 |

### 5.3 Push 时的丢弃行为

```
初始: [1, 2, 3, 4, 5]
          CurrentIdx=2 (已执行 1,2,3)

Push(6) → 丢弃 4,5 → [1, 2, 3, 6]
                          CurrentIdx=3
```

### 5.4 逆操作设计

每条命令的 `Inverse` 字段可选，描述如何"撤销"该操作：

```json
// sketch_rectangle 的逆操作
"inverse": {"operation": "delete_sketch_by_seq", "sketch_seq": 1}

// pad_extrude 的逆操作
"inverse": {"operation": "delete_feature_by_seq", "feature_seq": 2}
```

**逆操作不直接执行**，而是作为一个提示传递给 MCP Server，MCP Server 根据提示执行实际撤销。这样保持命令栈不依赖 FreeCAD 的具体实现。

---

## 6. 文件持久化

### 6.1 Save 方法

```go
// Save 原子写入：防止写入中断导致文件损坏。
func (cs *CommandStack) Save(path string) error {
    cs.mu.RLock()
    defer cs.mu.RUnlock()

    data, err := json.MarshalIndent(cs, "", "  ")
    if err != nil {
        return fmt.Errorf("commandstack: marshal: %w", err)
    }

    // 原子写入
    tmpPath := path + ".tmp"
    if err := os.WriteFile(tmpPath, data, 0644); err != nil {
        return fmt.Errorf("commandstack: write tmp: %w", err)
    }
    if err := os.Rename(tmpPath, path); err != nil {
        return fmt.Errorf("commandstack: rename: %w", err)
    }
    return nil
}
```

### 6.2 自动保存

通过 `AutoSave` 选项启动 goroutine，在每次 Push/Undo/Redo 后自动写入：

```go
type Option func(*CommandStack)

func WithAutoSave(path string) Option {
    return func(cs *CommandStack) {
        cs.autoSavePath = path
    }
}
```

---

## 7. 与 ContextSnapshot 的集成

### 7.1 元数据嵌入

`CommandMetadata` 复用了 `snapshot.ContextSnapshot` 的部分字段，但不直接嵌入整个快照：

```go
// 从 ContextSnapshot 创建 CommandMetadata
func MetadataFromSnapshot(snap *snapshot.ContextSnapshot) CommandMetadata {
    return CommandMetadata{
        DesignGoal:  snap.Goal,
        Decisions:   snap.Decisions,
        Findings:    snap.Findings,
        Progress:    snap.Progress,
        Constraints: snap.Constraints,
    }
}

// 合并回 ContextSnapshot（用于 Provider.Export）
func (m *CommandMetadata) MergeInto(snap *snapshot.ContextSnapshot) {
    if m.DesignGoal != "" {
        snap.Goal = m.DesignGoal
    }
    snap.Decisions = append(snap.Decisions, m.Decisions...)
    snap.Findings = append(snap.Findings, m.Findings...)
    snap.Constraints = append(snap.Constraints, m.Constraints...)
}
```

### 7.2 Token 预算感知

LLM 上下文窗口有限，命令栈在注入时需要控制大小：

```go
// ForPrompt 生成供 LLM 阅读的文本摘要，受 token budget 约束。
func (cs *CommandStack) ForPrompt(budget int) string {
    if cs.Count() == 0 {
        return "当前设计为空。"
    }

    var b strings.Builder
    b.WriteString("## 当前设计状态\n\n")

    // 元数据摘要（低开销）
    if cs.Metadata.DesignGoal != "" {
        b.WriteString(fmt.Sprintf("**目标**: %s\n", cs.Metadata.DesignGoal))
    }

    // 计算可容纳的命令数
    avgTokensPerCmd := cs.averageTokenCount()
    if avgTokensPerCmd == 0 {
        avgTokensPerCmd = 20 // 默认估算
    }
    maxCmds := (budget - estimateMetadataTokens(cs)) / avgTokensPerCmd
    if maxCmds < 3 {
        maxCmds = 3 // 最少显示 3 条
    }

    // 最近 N 条命令
    start := max(0, cs.CurrentIdx-maxCmds+1)
    b.WriteString(fmt.Sprintf("**操作历史**: 共 %d 条（显示 %d 条）\n\n", cs.Count(), cs.CurrentIdx-start+1))

    for i := start; i <= cs.CurrentIdx; i++ {
        cmd := cs.Commands[i]
        b.WriteString(fmt.Sprintf("%d. **%s** — %s\n",
            cmd.Seq, cmd.Op, truncateString(cmd.AIReasoning, 60)))
    }

    if cs.CurrentIdx < len(cs.Commands)-1 {
        b.WriteString(fmt.Sprintf("\n> 有 %d 条已撤销的操作\n", len(cs.Commands)-1-cs.CurrentIdx))
    }

    return b.String()
}
```

---

## 8. Provider 实现

实现 `provider.Provider` 接口，使命令栈成为 Seele 上下文来源之一：

```go
// provider/commandstack.go

package provider

import (
    "context"
    "github.com/RedHuang-0622/seelex/commandstack"
    "github.com/RedHuang-0622/seelex/snapshot"
)

// CommandStackProvider 从命令栈导出上下文。
type CommandStackProvider struct {
    stack *commandstack.CommandStack
    name  string
}

func NewCommandStackProvider(stack *commandstack.CommandStack) *CommandStackProvider {
    return &CommandStackProvider{
        stack: stack,
        name:  "commandstack",
    }
}

func (p *CommandStackProvider) Name() string { return p.name }

// Export 从命令栈导出 ContextSnapshot。
// - Goal / Constraints 来自 CommandMetadata
// - PendingWork 来自状态为 StatusPending 的命令
// - 结构化的历史摘要填充到 Findings
func (p *CommandStackProvider) Export(_ context.Context) (*snapshot.ContextSnapshot, error) {
    snap := &snapshot.ContextSnapshot{
        Goal:       p.stack.Metadata.DesignGoal,
        Constraints: p.stack.Metadata.Constraints,
        Findings:   p.stack.Metadata.Findings,
    }

    // 添加命令栈摘要
    snap.Findings = append(snap.Findings,
        commandstack.FormatSummary(p.stack))

    // 待完成的命令
    for _, cmd := range p.stack.Commands {
        if cmd.Status == commandstack.StatusPending {
            snap.PendingWork = append(snap.PendingWork,
                fmt.Sprintf("%s: %s", cmd.Op, cmd.AIReasoning))
        }
    }

    return snap, nil
}
```

---

## 9. 线程安全

所有命令栈操作使用 `sync.RWMutex`：

| 方法 | 锁类型 | 说明 |
|------|--------|------|
| Push | 写锁 | 修改 Commands 和 CurrentIdx |
| Undo/Redo | 写锁 | 修改 CurrentIdx |
| Current/Peek | 读锁 | 只读 |
| Length/Count | 读锁 | 只读 |
| Marshal | 读锁 | 读取全部字段 |
| Unmarshal | 写锁 | 覆盖整个结构 |
| Save/Load | 取决于内部调用 | 见方法定义 |
| Snapshot | 读锁 | 深度拷贝全部字段 |

---

## 10. 完整代码骨架

```
commandstack/
├── commandstack.go      # CommandStack, Command, CommandMetadata 定义
├── commandstack_test.go # 序列化反序列化测试
├── ops.go               # 操作类型常量和验证
├── stack.go             # Stack 接口 + 实现（Push/Undo/Redo/Current/Peek）
├── stack_test.go        # 栈操作测试（undo/redo 边界）
├── persist.go           # Save/Load（原子写入）
├── persist_test.go      # 文件读写测试
├── prompt.go            # ForPrompt（token 预算感知的文本摘要）
├── prompt_test.go       # 文本摘要测试
├── snapshot.go          # Snapshot（深度拷贝）
└── provider.go          # Provider 接口实现（可选，也可放在 provider/ 包）
```

---

## 附录：使用示例

```go
// 创建命令栈
cs := commandstack.New(
    commandstack.WithSessionID("cad-001"),
    commandstack.WithMetadata(commandstack.CommandMetadata{
        DesignGoal: "设计一个 100×50×30mm 的外壳",
        Constraints: []string{"壁厚 ≥ 2mm"},
        Units: "mm",
    }),
    commandstack.WithAutoSave(".seelex/cad/cad-001.cmdstack.json"),
)

// 追加命令
cmd1 := commandstack.Command{
    ID:        uuid.New().String(),
    Seq:       1,
    Op:        commandstack.OpSketchRectangle,
    Params:    json.RawMessage(`{"plane":"XY","length":100,"width":50}`),
    Status:    commandstack.StatusExecuted,
}
cs.Push(cmd1)

// Undo
undone, err := cs.Undo()

// Redo
redone, err := cs.Redo()

// 导出供 LLM 阅读
prompt := cs.ForPrompt(500)
fmt.Println(prompt)
```
