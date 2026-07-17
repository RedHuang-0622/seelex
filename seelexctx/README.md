# `seelexctx` — 会话上下文承袭

为 Seelex 提供跨 Engine 的上下文导出/导入能力，支撑 A2A 子代理调度时的上下文继承。

## 架构（平级包）

```
seelex/
├── seelexctx/     Export/Import 向后兼容（package seelexctx）
├── snapshot/      ContextSnapshot 类型 + Format + Validate + Builder
├── provider/      Provider 接口 + EngineProvider + TraceProvider
├── compactor/     基于 token 预算的三级上下文压缩
└── merger/        MergeBack 双向上下文合并
```

## 子包说明

### `snapshot`

```
import "github.com/RedHuang-0622/seelex/snapshot"

snap := &snapshot.ContextSnapshot{
    SourceSessionID: "sess_xxx",
    Goal:            "实现功能 X",
}
snap.AddDecision("方案A", "性能更好").AddFinding("需优化内存")
output := snap.Format()      // → 可注入 system prompt 的结构化文本
snap.Validate()               // → nil or *ValidationError
```

### `provider`

```
import "github.com/RedHuang-0622/seelex/provider"

p := provider.NewEngineProvider(eng)
snap, _ := p.Export(ctx)

t := provider.NewTraceProvider(eng)
traceSnap, _ := t.Export(ctx)  // 从 tracer.Tree 自动提取
```

### `compactor`

```
import "github.com/RedHuang-0622/seelex/compactor"

c := compactor.NewCompactor()
compressed, _ := c.Compact(snap, 300)
// ≥500 → 全量 | 200~499 → 摘要 | <200 → 极简
```

### `merger`

```
import "github.com/RedHuang-0622/seelex/merger"

m := merger.NewMerger()
m.MergeBack(parentSnap, childSnap)
// Findings/Decisions → append | Progress → 替换 | Constraints → 去重
```

### 向后兼容 API

```
import "github.com/RedHuang-0622/seelex/seelexctx"

snap := seelexctx.Export(eng)
seelexctx.Import(subEng, snap)
// Import 自动调用 seelectx.NeedCompression + TrimHistory 做预算检查
```

## 与 Seele 的集成

| Seele 方法 | 使用方 |
|------------|--------|
| `seelectx.EstimateTokens` | compactor |
| `seelectx.NeedCompression` | seelexctx/bridge.go (Import) |
| `seelectx.TrimHistory` | seelexctx/bridge.go (Import) |
| `engine.ExportTrace` → `tracer.Tree` | provider/trace.go (TraceProvider) |

## 设计原则

1. **接口化** — Provider/Compactor/Merger 三接口
2. **复用 Seele** — token 估算、历史压缩委托 seelectx
3. **Copy-on-write** — 不修改原始快照
4. **向后兼容** — Export/Import 签名不变
