# BM-005 首个切片设计门禁

日期：2026-10-26

状态：**设计已收敛，首个切片 `review_files` 注册归属已实施并验证。**

## 1. 领域归属

`show_files` 由三部分组成：

| 部分 | 当前所有者 | 最终所有者 | 判定 |
|---|---|---|---|
| 工具名、描述、schema、参数校验、回执 | `backend/tool/review` | `backend/tool/review` | 工具形状属于能力实现域 |
| 文件读取、敏感/越界裁决与会话请求落点 | Application `Service` + workspace port | Application + workspace port | 跨域动作与会话作用域由应用用例编排 |
| `runtime.RegisterTool(...)` 登记动作 | `backend/engine/assemble/review_files.go` 薄包装 | `wiring/registry` | 纯构造/注册，不属于 Runtime 领域 |

因此首个切片不移动工具语义，只把**注册调用所有权**迁到 `wiring/registry`。
旧 `assemble.RegisterReviewFilesTool` 在迁移期保留 D1 一行转发。

## 2. 候选方案

| 维度 | A：`review_files` 注册归属 | B：`teamwork_job_namer + signals` |
|---|---|---|
| 耦合度 | 低：只把注册动作接到已存在的 domain helper | 中：namer 纯，但 signals 连接 jobs 上游、Application 订阅与 Runtime 停机顺序 |
| 内聚性 | 高：工具定义、注册与落点三段各归其位 | namer 高；signals 混有扇出基础设施，并非 Teamwork 聚合不变量 |
| 可测试性 | 高：可用 fake `ToolRegistrar` 验证注册形状，不动 Runtime | namer 容易；signals 需并发/生命周期测试 |
| 实现成本 | 小：一个 consumer interface + 一个注册函数 | 中：改 `runtime_teamwork.go` 构造点与停机链 |
| 高冲突文件 | 不触碰 `runtime*.go` | 必须触碰 `runtime_teamwork.go` |
| 可回滚性 | 高：恢复 registry 一行转调旧 wrapper | 中：要同时回退构造、字段类型与测试 |

## 3. 推荐：方案 A

理由：

1. 符合“工具注册 ⇒ `wiring/registry`”的固定归属；
2. 不改变 Runtime 生命周期、WorkUnit、Teamwork、Subagent 或 Session 状态；
3. 旧 wrapper 可保留 D1 一行转发，行为完全兼容；
4. 不触碰 `runtime*.go` 高冲突车道。

最大风险：注册函数在两处调用。规避方式是两处都只调用
`review.RegisterShowFilesTool(registrar, port)`，不得各自重复拼工具定义。

## 4. 契约与 struct

### `backend/tool/review.ToolRegistrar`

消费方本地端口，只包含：

```go
type ToolRegistrar interface {
    RegisterTool(
        name, description string,
        inputSchema map[string]interface{},
        handler func(context.Context, string) (string, error),
    )
}
```

真实实现：

- `*backend/engine/assemble.Runtime`（结构满足，无需适配器）；
- 测试 fake（记录一次登记参数）。

真实调用方：

- `wiring/registry.RegisterReviewFilesTool`；
- 迁移期 `backend/engine/assemble.RegisterReviewFilesTool` 一行转发。

### `review.RegisterShowFilesTool`

唯一注册实现：

```go
func RegisterShowFilesTool(registrar ToolRegistrar, port ShowFilesPort)
```

职责仅为组装既有 `ShowFilesToolName` / `ShowFilesDescription` /
`ShowFilesSchema` / `ShowFilesHandler` 并调用一次 `registrar.RegisterTool`。

无新增 DTO、JSON、事件、持久化字段。

## 5. 不变式

- `show_files` 工具名、schema、回执字段逐字不变；
- 单次最多 10 份、超限显式报错、空清单报错由既有 handler 保持；
- workspace containment / sensitive / binary 裁决仍在原端口链；
- Runtime 的 `scopedToolsReady` 防重复注册行为不变；
- 不新增状态所有者，不引入后台 goroutine。

## 6. 下游影响矩阵

| 层 | 影响 |
|---|---|
| `backend/tool/review` | 新增注册 helper；工具行为不变 |
| `backend/engine/assemble` | wrapper 降为一行转发，保留 alias |
| `wiring/registry` | 直接调用 domain helper，不再经 assemble wrapper |
| Application | 无变化 |
| Event / store / wire | 无变化 |
| 测试 | 新增 fake registrar 单测；既有 assemble 集成用例保持不变 |
| README | review、registry、assemble 迁移动态；BM-005 记录更新 |

## 7. 迁移顺序与门禁

1. 新增结构红测：要求 `review.RegisterShowFilesTool` 存在，registry 直接使用它，assemble
   wrapper 仅转发；
2. 在 `backend/tool/review` 增加 `ToolRegistrar` 与 `RegisterShowFilesTool`；
3. 增加 domain 红测后转绿，验证工具名/schema/handler 只注册一次；
4. registry 改直接调用；
5. assemble wrapper 降为 D1 一行转发；
6. 更新 README、inventory、implementation；
7. 运行局部测试、依赖门禁与构建。

## 8. 兼容与回退

- 旧面：`assemble.RegisterReviewFilesTool(*Runtime, ReviewFilesPort)`；
- 新面：`registry.RegisterReviewFilesTool(*Runtime, *application.Service)` +
  `review.RegisterShowFilesTool`；
- 等级：实现时 D1（一行转发），生产调用切换后当前为 D2；
- 删除里程碑：BM-007（`assemble.Runtime` 与旧路径退场时）；
- 归零验证：生产调用只命中 `wiring/registry`，旧 wrapper 仅剩测试/兼容调用；
- 回退：registry 恢复转调旧 wrapper，并保留 domain helper；无状态可回滚。

## 9. Inventory 修正建议

| 文件 | 原分类 | 建议 | 理由 |
|---|---|---|---|
| `review_files.go` | `wiring/registry` | 保持 | 纯注册动作，首个切片按此迁移 |
| `teamwork_job_namer.go` | `backend/multiagent/teamwork` | 保持 | 产品句柄命名策略属于 Teamwork 词汇 |
| `teamwork_job_signals.go` | `backend/multiagent/teamwork` | 改为 `wiring/runtime` | 管理上游单读者通道、订阅扇出与停机顺序，不维护 Teamwork 聚合不变量 |

该修正只登记，不在首个切片实施。

## 10. 循环依赖检查

目标方向：

```text
wiring/registry
  ├─→ backend/engine/assemble（Runtime 类型，迁移期）
  └─→ backend/tool/review
        ├─→ contract/dto
        └─→ consumer ToolRegistrar（Runtime 结构满足）

backend/engine/assemble
  └─→ backend/tool/review（D1 转发）
```

不存在 `backend/tool/review -> assemble` 或 `assemble -> wiring` 反向边。

## 11. 实施结果

- `review.ToolRegistrar` 与 `review.RegisterShowFilesTool` 已落地；
- `wiring/registry` 已直接调用域 helper；
- assemble 旧入口已降为 D1 一行转发，并有
  `var _ review.ToolRegistrar = (*Runtime)(nil)` 编译期结构断言；
- 新增 fake registrar 用例验证工具名、描述、schema、handler 各注册一次；
- 新增 contract 结构门禁验证生产路径归 registry、旧路径仅转发；
- `teamwork_job_signals.go` 的迁移分类已修正为 `wiring/runtime`。
