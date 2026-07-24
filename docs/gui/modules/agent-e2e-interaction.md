# Agent E2E 交互模块详细设计

> 状态：拟议方案
> 产品需求：`CAP-E2E`
> 总体架构：[`../../arch/agent-workbench-architecture.md`](../../arch/agent-workbench-architecture.md)

## 1. 目标与边界

本模块验证用户看到的完整 Agent 交互链，而不是把“所有测试都换成真实模型”。主门禁必须确定性、可复现、无外部服务依赖。

覆盖链路：

```text
输入任务
  → Chat running / streaming
  → Tool start / complete
  → Approval open / resolve
  → DSL Card 出现在对话区
  → FileLink/ArtifactLink 导航右栏 Workspace
  → Artifact 可追溯
  → Session 保存/恢复
```

不覆盖或不强制：

- 每个 PR 调用真实付费模型；
- 用截图像素替代业务断言；
- 依赖测试顺序、固定 sleep 或共享用户目录；
- 让 fake 前端重写 Application 业务规则；
- 把 Playwright Chromium 等同于真实 Wails WebView。

## 2. 分层测试架构

| 层 | 运行对象 | 主要证明 | PR 门禁 |
|----|----------|----------|:-------:|
| L0 Schema/Unit | JSON schema、Go/JS 纯函数 | 输入和算法边界 | 是 |
| L1 Core scenario | 真实 `application.Service` + scripted ports | Agent 状态机、事件、审批、Session | 是 |
| L2 Browser journey | 真实 `gui/frontend/dist` + fake Wails Bridge | DOM、键盘、scroll、modal、Card/Workspace 联动 | 是 |
| L3 Wails smoke | production binary + scripted/local backend | Wails binding、系统 WebView、startup/shutdown | nightly/发布 |
| L4 Live Agent | production stack + 真实 provider | 模型可用性与实际任务效果 | opt-in nightly |

L1 与 L2 使用同一个 `scenario-v1` 语义和 fixture ID。它们的 driver 不同，但预期用户旅程一致。

## 3. Scenario v1

计划新增 `schemas/agent-scenario-v1.schema.json`。Scenario 是数据，不允许内嵌 JavaScript、shell 或 Go callback。

```json
{
  "schema_version": "seelex.scenario/v1",
  "id": "approval-card-workspace-resume",
  "workspace_fixture": "fixtures/workspaces/go-small",
  "initial": {
    "plugin": "default",
    "effort": "high",
    "session_id": "session_e2e_1"
  },
  "engine_script": [
    {
      "on_user": "检查性能并展示结果",
      "emit": [
        {
          "type": "assistant.delta",
          "value": "我先检查实现。"
        },
        {
          "type": "tool.call",
          "name": "read_file",
          "arguments_fixture": "read_target.json",
          "result_fixture": "read_target.txt"
        },
        {
          "type": "approval.request",
          "id": "approval_e2e_1",
          "risk": "medium"
        },
        {
          "type": "tool.call",
          "name": "render_card",
          "arguments_fixture": "performance_card.json",
          "result_fixture": "render_card_ok.json"
        },
        {
          "type": "artifact.register",
          "fixture": "report_artifact.json"
        }
      ]
    }
  ],
  "steps": [
    {
      "action": "submit",
      "text": "检查性能并展示结果"
    },
    {
      "expect": "tool_status",
      "tool": "read_file",
      "status": "success"
    },
    {
      "expect": "interaction",
      "id": "approval_e2e_1"
    },
    {
      "action": "resolve_interaction",
      "option": "allow"
    },
    {
      "expect": "conversation_card",
      "surface_id": "card_perf_review"
    },
    {
      "action": "card_action",
      "surface_id": "card_perf_review",
      "action_id": "reveal_impl"
    },
    {
      "expect": "workspace_preview",
      "resource_id": "wsres_7c51"
    },
    {
      "action": "resume_session",
      "session_id": "session_e2e_1"
    },
    {
      "expect": "conversation_card",
      "surface_id": "card_perf_review"
    }
  ]
}
```

## 4. Scenario runner

### 4.1 Go Core runner

计划目录：

```text
e2e/scenario/
  schema.go
  loader.go
  runner.go
  scripted_engine.go
  ports.go
  recorder.go
  assertions.go
```

Runner 组装真实：

- `application.Service`；
- `EventHub`；
- `ApprovalBroker`；
- Card presentation coordinator；
- Workspace guarded adapter；
- Session transcript/presentation stores。

替换为 scripted fake 的只有外部或非确定性边界：Engine/provider、clock、ID generator、OS opener。Workspace 使用每个 test 独立临时目录复制 fixture，而不是 fake 掉 PathGuard。

### 4.2 Scripted Engine

Engine script 按用户输入和当前阶段消费事件。每项 emit 都有明确 barrier：

| emit | Runner 行为 |
|------|-------------|
| assistant.delta | 调用真实 ChatStream callback |
| tool.call | 触发真实 Tool hooks/handler；args/result 来自 fixture |
| approval.request | 真实 ApprovalBroker 阻塞，等待 scenario resolve step |
| engine.error | 返回指定稳定错误码 |
| wait_for_cancel | 等 context cancel，不使用 sleep |

脚本未消费完、顺序不符或多出调用都视为失败，防止测试“没走到目标路径也通过”。

### 4.3 Event recorder

每次运行记录：

```text
artifacts/<scenario>/<run>/
  scenario.json
  events.jsonl
  snapshots/
    000-initial.json
    010-after-submit.json
    ...
  engine-script-state.json
  workspace-audit.jsonl
  result.json
```

Event 记录包含 seq、revision、request/turn/item ID 和 payload digest。包含文件内容、token、密钥或审批 preview 时按字段规则脱敏，不把完整敏感数据上传 CI。

## 5. Playwright fake Wails Bridge

### 5.1 目标

浏览器测试加载仓库真实 `gui/frontend/dist`，不复制 HTML 或 renderer。Fake 只模拟 Wails transport：

```text
window.go.gui.Bridge.*
window.runtime.EventsOn(name, callback)
```

Bridge method 的返回值和事件序列由 scenario driver 提供。业务期望来自 Core scenario 生成的 fixture 或共享 contract，不在浏览器 fake 中重新实现 Skill、权限、PathGuard 等规则。

### 5.2 目录

```text
gui/e2e/
  package.json
  playwright.config.mjs
  static-server.mjs
  fake-wails.mjs
  scenario-driver.mjs
  selectors.mjs
  journeys/
    approval-card-workspace.spec.mjs
    error-recovery.spec.mjs
    session-resume.spec.mjs
```

Playwright 依赖只属于 `gui/e2e` 开发/CI 工具，不进入 embedded runtime bundle。

### 5.3 稳定选择器

优先级：

1. ARIA role/name；
2. 业务实体属性，例如 `data-item-id`、`data-surface-id`、`data-resource-id`；
3. 少量 `data-testid` 用于无稳定语义的容器。

禁止依赖 CSS 布局 class、数组位置、动画完成时刻和本地化后的自由文本作为唯一选择器。

### 5.4 等待规则

- 等 DOM 状态、Event seq、Bridge 调用或可见元素；
- 禁止 `waitForTimeout` 作为同步机制；
- 动画在 test profile 中通过 `prefers-reduced-motion` 关闭；
- 每个 step 有独立 timeout 和诊断；
- modal、focus、scroll 断言在 requestAnimationFrame 稳定后执行。

## 6. P0 黄金旅程

### E2E-J01 基础 Chat

提交 → user/assistant item → delta → running false → send/stop 状态正确。

### E2E-J02 Tool 与审批

Tool RUN → approval modal → allow → Tool OK → interaction closed。补充 reject/cancel/timeout 失败路径。

### E2E-J03 DSL 对话卡片

`render_card` → Card 出现在对应 turn 的中间对话区 → patch 更新局部节点 → invalid mutation 显示 ErrorCard。明确断言右栏没有 DSL Card DOM。

### E2E-J04 Card → Workspace

点击 FileLink → Core action resolution → 右栏切 Files → 展开父目录 → 文件预览定位指定行；Card 保持在对话区且滚动状态不被右栏刷新破坏。

### E2E-J05 Artifact

审批写操作 → Artifact registered → 右栏 Artifacts 更新 → ArtifactLink 定位 → hash/source turn/producer 可见。

### E2E-J06 Session resume

完成带 Tool/Card/Artifact 的 turn → 新建/切换 → resume → item 顺序、Card revision、Workspace resource 状态一致。

### E2E-J07 Resync

故意跳过一个 Event seq → 客户端拉 Snapshot → 不重复 delta/Card patch → 页面恢复且可继续提交。

## 7. 失败路径矩阵

| 场景 | 预期 |
|------|------|
| Engine 返回错误 | Chat error 可见，running 清零，可重新提交 |
| cancel 与 tool running 竞争 | 过期 delta/tool complete 不污染新 request |
| approval reject | 工具不执行，Card/Artifact 不伪造成功 |
| invalid DSL | ErrorCard/诊断，Conversation 继续 |
| Card revision conflict | 保留旧 Card，展示更新失败，不 last-write-wins |
| Workspace path denied | 右栏错误与重试，不中断 Chat |
| resource ID 过期 | FileLink unavailable，Card 仍可读 |
| Session partial bundle | 回退最后完整 generation 或只读文本历史 |
| Event seq gap | Snapshot resync，稳定 item 不重复 |
| Bridge method reject | toast、权威 Snapshot 回滚局部预览 |

## 8. Wails smoke

目标是验证 Playwright 无法证明的桌面容器差异：

1. production tags 构建；
2. 以临时 ProjectRoot 和 scripted backend 启动；
3. 等待 `seelex:ready`；
4. 调用一次 Submit 并观察 Event relay；
5. 检查 embedded assets、Bridge binding 和窗口无启动错误；
6. 正常 shutdown，确认 goroutine/process 无泄漏。

首选 Windows runner。若 hosted runner 无可靠交互桌面，拆为：

- PR：binary startup/health + Bridge integration；
- nightly self-hosted：WebView 点击与截图。

不能因为 smoke 不稳定而删除 L1/L2 主门禁。

## 9. Live Agent nightly

真实模型场景用于度量效果，不验证确定性协议细节：

- 由 workflow_dispatch 或 nightly 触发；
- 使用专用低权限测试账号与临时 Workspace；
- 单 run 设 token、费用、turn、tool 和 wall-clock 上限；
- 禁止访问生产 secret、真实用户目录和外网任意 URL；
- 断言结构化 outcome，例如 Card schema 有效、Artifact 存在、无越界审计；
- 模型自由文本不做完整字符串匹配；
- 失败先分类 provider outage / model quality / product regression，不直接阻塞所有 PR。

## 10. CI 设计

计划扩展 `.github/workflows/ci.yml`：

```text
gui-e2e (ubuntu)
  ├─ setup Node + Playwright browser cache
  ├─ generate/share scenario fixtures
  ├─ run Playwright P0 journeys
  └─ upload trace/screenshots/report on failure

agent-scenarios (ubuntu)
  ├─ go test ./e2e/scenario/... -count=1
  ├─ selected scenarios -count=30 (scheduled or merge queue)
  └─ upload events/snapshot artifacts on failure

wails-smoke (windows)
  └─ nightly/release or required after stability target
```

性能预算：

| Job | P95 目标 |
|-----|---------:|
| Core P0 scenarios | <60s |
| Playwright P0 journeys | <5min |
| Windows smoke | <5min |
| 30-run stability | <15min，scheduled/merge queue |

## 11. Flake 控制

- clock、ID、Engine chunk 和 Workspace fixture 全部注入；
- 每个 scenario 独立临时目录、Session store 和 EventHub；
- 不依赖 runner 时区、locale、网络、用户 home 或 git 全局配置；
- 并发测试使用 barrier/channel/condition，不使用经验 sleep；
- retry 只用于收集证据，第一次失败仍计入 flake 指标；
- 连续 flaky 用例不能长期标记 known issue，必须隔离责任人和期限；
- screenshot 作为诊断，DOM/DTO/assertion 才是通过标准。

## 12. 安全与隐私

- fixture 不含真实 API key、账户文件或用户数据；
- CI trace 过滤 Authorization、token、secret、完整绝对路径和审批敏感 preview；
- 浏览器外链 opener 使用 fake，不访问真实 URL；
- Workspace 写 fixture 只能位于已验证临时根；
- destructive scenario 的目标在构造后解析确认，结束时只删除该临时根；
- live tests 使用最低权限账号、专用预算与自动过期 secret。

## 13. 计划改动位置

| 层 | 文件/目录 | 变更 |
|----|-----------|------|
| Schema | `schemas/agent-scenario-v1.schema.json` | scenario 合约 |
| Go runner | `e2e/scenario/` | loader、driver、ports、recorder、assertions |
| Fixtures | `e2e/fixtures/` | engine、workspace、card、artifact |
| Browser | `gui/e2e/` | Playwright、fake Wails、journeys |
| Frontend | `index.html`、renderer modules | 稳定 ARIA/entity selector |
| Wails | `gui/`、`scripts/` | smoke backend/launcher |
| CI | `.github/workflows/ci.yml` | scenario、Playwright、smoke jobs |
| Docs | `docs/gui/ci-and-testing.md` | 门禁命令、失败定位和 artifacts |

## 14. 验收追溯

| PRD | 设计落点 |
|-----|----------|
| E2E-001 | scenario-v1 schema |
| E2E-002 | Go Core runner 使用真实 Service |
| E2E-003 | Playwright fake Wails transport |
| E2E-004 | J01—J07 journeys |
| E2E-005 | event/snapshot/trace artifacts |
| E2E-006 | opt-in live nightly + budgets |
| E2E-007 | Windows Wails smoke |
