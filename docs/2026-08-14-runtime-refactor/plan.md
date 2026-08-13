# runtime 彻底重构方案（thin runtime）

日期：2026-08-14
状态：**规划（尚未实施）**
目标：把 `seelebridge/runtime.go`（57KB / 111 个顶层声明）从"巨型组合根单文件"收敛为
"骨架组合根 + 按装配域物理拆分 + 端口文件纯化"的形态，保持依赖方向与公开 API 不变。

## 0. 现状与问题（有证据）

| 文件 | 现状 | 问题 |
|---|---|---|
| `runtime.go` | 57KB，111 个顶层声明 | 单一文件承载 5 类职责：组合骨架、主会话装配、plan 接线、上下文接线（Assembler/Controller/Compressor）、账号路由、工具注册表与可见性策略、Deps 闭包工厂 |
| `ports.go` | 21KB | 混入内部 actor 方法（`enqueueSubagentContext`/`mergeBackIntoParent`/`subagentContextDropped`）与 `mcpRegistryAdapter`，不满足"纯公开端口"心智 |
| 根测试 | `runtime_test.go` 31KB 等 27 个白盒文件 | 未按装配域分组，联调面混杂 |

根因：上一轮"只留 runtime.go + ports.go"的终极方案矫枉过正——约束的是依赖方向与域归属，
却被执行成了"物理上只许两个文件"，导致组合根膨胀。

## 1. 目标与约束

- 保持"组合根 + 端口"心智：`Runtime` 结构体仍是唯一组合根，字段全部 Manager。
- 依赖方向不变：根包 → 子包单向；跨域协作只走 Deps 闭包或端口接口。
- 公开 API（`application/contract`）零变更；行为零变更（每阶段全绿）。
- 不复现上帝类/上帝包：拆的是**文件与装配职责**，不造新上帝。
- **修订上一轮约束**：根目录从"只许 2 个文件"放宽为"组合根骨架 + 端口文件 + 装配域文件"
  （`runtime_*.go` 仍属 composition root 的 package seelebridge，不改变依赖方向）。

## 2. 目标文件结构（package seelebridge 内物理拆分）

```text
seelebridge/
  runtime.go           组合根骨架：Runtime 结构体 + RuntimeConfig + NewRuntime + Shutdown（约 15KB）
  ports.go             纯公开端口：仅 application/contract 端口实现 + 兼容类型别名
  runtime_plan.go      plan 执行域装配与委托（SetPlan*/Replan/事件通道/分支账号/NodeFactory/plan 工具）
  runtime_context.go   上下文接线（Assembler/Controller/Compressor/窗口/stack/project/memory/归档）
  runtime_account.go   账号装配与路由（pool 注册/选择器/Provider/SetProvider/Accounts/Limits）
  runtime_tools.go     工具注册表装配与可见性策略（RegistryState/builtins/RegisterTool/AllTools/VisibilityPolicy）
  runtime_session.go   主会话装配（NewMainSession*/History/归档/诊断观察者/merge-back 内部方法）
  runtime_deps.go      Deps 闭包工厂（forkDeps/nodeDeps/taskToolsDeps/scopedToolsDeps + docker/fork/node/worktree 接线）
```

## 3. 分阶段执行

### R1 文件物理拆分（纯搬移，零行为变化，低风险）

按上表切分 `runtime.go`，import 各自收敛；每批 `gofmt -l` 空 + `go vet` 绿后提交。

验证：`go build ./...` + `go vet ./...` + `go test ./seelebridge/... -count=1`。

### R2 ports.go 纯化

- 把 `enqueueSubagentContext`/`mergeBackIntoParent`/`subagentContextDropped` 移入
  `runtime_session.go`；`mcpRegistryAdapter` 移入 `runtime_tools.go`。
- ports.go 只留公开端口 + 兼容别名；加编译期断言
  `var _ application.RuntimePort = (*Runtime)(nil)`（需确认 adapters 侧接口名）防回归。

验证：同上 + `git diff --check`。

### R3 装配逻辑 Manager 化（收益大、风险中，逐项评估）

把 Runtime 直接持有的"裸状态"收敛为域 Manager（委托留 Runtime）：

1. **account.Manager（建议做）**：`branchMu/selectedAccountID/providerFilter/accountSpecs/
   accountLimits` 收进 account 域。`NewManager(specs, pool)` + 方法
   `Select/SetProvider/Accounts/Limits/Selector`；`runtime_account.go` 只剩委托。
2. **visibility policy（建议做）**：`seelexVisibilityPolicy/isPlanTool/nodeScopeExcludedTool`
   移入 tools 域（`Policy` 类型，deps：`GoalSkillActive func() bool` + `plugin Filter` +
   scope 读取闭包）；runtime 装配时构造一次。
3. **context 接线（评估后决定）**：`seelexAssembler/seelexController/seelexCompressor`
   依赖 Runtime 大量字段（completer/window/ctxStore/turnArchiver…），Manager 化改动面大。
   **若评估改动面超过阈值则保留 `runtime_context.go` 内装配方法，不做语义迁移**——物理拆分
   已解决可读性，语义迁移只做收益明确的部分。

约束：每项独立提交；白盒测试改动同步在对应阶段完成；不用并行子代理做紧耦合迁移。

### R4 测试归位与文档

- `runtime_test.go`（31KB）按装配域拆为 `runtime_account_test.go`/`runtime_session_test.go`/
  `runtime_plan_test.go`/`runtime_tools_test.go`/`runtime_context_test.go`。
- 根 README 与各文件顶部注释同步；文档契约测试通过。

## 4. 验证门（每阶段）

```text
gofmt -l .            → 空
go build ./...        → 绿
go vet ./...          → 绿（含 writestring 等分析）
go test ./... -count=1 -timeout=120s → 全绿
go test ./e2e/... -run 'Docs|ModuleReadme' → 绿
```

## 5. 风险与对策

| 风险 | 对策 |
|---|---|
| R3 context 接线改动面失控 | 预设阈值：改动 > 400 行或触碰 5 个以上域文件即降级为"保留 runtime_context.go"，先交付 R1+R2 |
| 白盒测试依赖内部字段 | 物理拆分不影响；Manager 化同步改测试（R4 一并） |
| 并行子代理破坏紧耦合迁移 | 全程顺序执行，每阶段绿点提交（沿用上次教训） |
| "只留两个文件"预期被打破 | 方案已显式修订该约束并给出理由（见 §1） |

## 6. 建议

- **R1 + R2 必做且低风险**，直接消除巨型文件与端口不纯两个债。
- **R3 先做 account.Manager 与 visibility policy**（边界清晰、收益明确），context 接线
  按评估决定。
- R4 收尾后，`seelebridge` 根包达到"骨架 + 端口 + 装配域文件"的稳态，后续新增能力
  只需在对应 `runtime_*.go` 或域包内扩展。
