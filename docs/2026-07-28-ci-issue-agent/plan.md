# CI 并行化与 Issue-Agent 闭环方案

## 设计目标

1. 将互不依赖的检查拆成独立 jobs，缩短反馈时间并减少三平台重复执行。
2. 保留一个稳定的 required check，避免每次调整 job 都修改分支保护规则。
3. CI 失败时自动创建或更新结构化 GitHub Issue，把 Issue 作为持久化故障队列。
4. 定时 Agent 只消费来源可信、状态明确的 Issue，先诊断，后续再开放自动修复 PR。
5. CI、故障上报和代码修复使用彼此隔离的最小权限。

## 当前结论

- 现有 `build` matrix 在 Ubuntu、Windows、macOS 上重复执行 gofmt、vet、全量测试和策略扫描。
- `gui-tests` 和 `race-and-coverage` 又重复部分 Go 测试。
- `release-safety` 每次 push/PR 都构建四个平台发行包，成本偏高。
- `seelebridge` 和 `seelexctx` 是主要慢包，本地约 70 秒和 50 秒。
- job 内 steps 必然串行；要获得并行收益，必须拆成多个 jobs。
- Issue 自动上报可行，但必须处理权限、去重、日志脱敏、可信来源和自动关闭。

## 方案比较

| 维度 | 方案 A：一个 CI workflow + 多 jobs + 独立 Reporter/Agent | 方案 B：多个领域 workflows + 外部 Agent 服务 |
| --- | --- | --- |
| CI 展示 | 一个 run 展示完整 job 图 | 安全、平台、GUI 分开显示 |
| required checks | 可用单一 `ci-gate` 汇总 | 需要多个 required checks 或聚合器 |
| Issue 去重 | Reporter 只监听一个 workflow | 需要跨 workflow 合并 |
| 权限隔离 | CI 只读，Reporter 写 Issue，Agent 写 PR | 最强，可完全脱离 GitHub runner |
| 实现成本 | 中 | 高 |
| 运维成本 | 低 | 需要常驻服务与密钥管理 |
| 适用阶段 | 当前仓库，推荐 | 多仓库或大规模自动修复 |

## 推荐方案

采用方案 A：保留一个主 `CI` workflow，拆分 jobs；新增 `CI Incident Reporter` 和 `CI Agent` 两个独立 workflow。

### 推荐 job 图

```text
policy-checks ───────────┐
go-tests ────────────────┤
frontend-tests ──────────┤
platform-build (matrix) ─┤
windows-gui-build ───────┤──> ci-gate
race-and-coverage ───────┤
security-scan ───────────┤
release-safety* ─────────┘
```

`release-safety` 建议仅在主分支、相关文件变更、手工触发或定时任务中运行。

### Job 职责

| Job | Runner | 内容 | 依赖 |
| --- | --- | --- | --- |
| `policy-checks` | Ubuntu | gofmt、go mod verify/tidy、go vet、密钥扫描、nil-return 扫描、配置白名单、actionlint | 无 |
| `go-tests` | Ubuntu | 普通全量 Go 测试 | 无 |
| `frontend-tests` | Ubuntu + Node 22 | JS syntax 和 Node tests，不重复 Go 全量测试 | 无 |
| `platform-build` | Linux/Windows/macOS matrix | 仅默认构建，`fail-fast: false` | 无 |
| `windows-gui-build` | Windows | production tags GUI 构建 | 无 |
| `race-and-coverage` | Ubuntu | 全仓 race、atomic coverage 和 artifact | 无 |
| `security-scan` | Ubuntu | govulncheck | 无 |
| `release-safety` | Ubuntu | 候选包和配置泄漏检查 | 无 |
| `ci-gate` | Ubuntu | `if: always()` 汇总 required jobs | 上述 jobs |

分支保护只绑定 `ci-gate`。内部 job 增删改名不会影响 required checks。

### 触发与并发

```yaml
permissions:
  contents: read

concurrency:
  group: ci-${{ github.workflow }}-${{ github.event.pull_request.number || github.ref }}
  cancel-in-progress: true
```

- 只取消同一 PR 或分支的过时 run。
- Reporter 忽略 `cancelled`，避免正常取消产生故障 Issue。
- GitHub-hosted jobs 使用独立 VM；self-hosted runner 必须限制并发，避免资源争用。

## CI 失败自动创建 Issue

新增 `.github/workflows/ci-incident.yml`：

```yaml
on:
  workflow_run:
    workflows: [CI]
    types: [completed]

permissions:
  actions: read
  contents: read
  issues: write
```

Reporter 不 checkout、不执行失败提交中的代码，只通过 GitHub API 获取 run 和 jobs 元数据。

### Issue 粒度与去重

推荐“一条失败 job 一个 Issue”。指纹：

```text
workflow + branch/PR + job-name
```

Issue 内写入隐藏标记：

```text
<!-- seelex-ci-incident:v1 fingerprint=CI:gui:race-and-coverage -->
```

- 没有相同指纹：创建 Issue。
- 已有相同指纹：追加本次 run 记录，不重复创建。
- 后续成功：关闭对应 Issue，并留言恢复 run URL。
- `cancelled` 和过时 run 不创建 Issue。

### Issue 数据结构

```yaml
schema: seelex-ci-incident/v1
workflow: CI
run_id: 123456
run_attempt: 1
event: pull_request
branch: gui
head_sha: abcdef
job: race-and-coverage
failed_steps:
  - Run Go race and coverage tests
run_url: https://github.com/.../actions/runs/123456
source_trust: same-repository
```

推荐 labels：`ci/failure`、`agent/ready`、`agent/in-progress`、`agent/blocked`、`agent/done`，以及 `area/go`、`area/gui`、`area/security`、`area/release`。

不要把完整日志粘贴进 Issue。Agent 使用 `run_id` 和 `actions:read` 按需读取失败 job 日志。

## 定时 Agent 消费 Issue

### 两种执行方式

| 维度 | GitHub Actions 定时 Agent | 外部或自托管 Agent 服务 |
| --- | --- | --- |
| 启动 | `schedule` + `workflow_dispatch` | webhook 或轮询 |
| 凭据 | GitHub Secrets/Environment | 独立密钥库 |
| 沙箱 | GitHub runner 或 self-hosted runner | 可使用专用容器或 VM |
| 状态 | 每次运行无状态 | 可维护持久队列 |
| 成本 | 低 | 中高 |
| 推荐阶段 | MVP | 多仓库和长期自动修复 |

推荐先采用 GitHub Actions 定时 Agent，并分阶段开放能力。

### 消费协议

1. 每 15 分钟查询 `ci/failure + agent/ready` 的 open Issues。
2. 领取一条后添加 `agent/in-progress`，移除 `agent/ready`。
3. 检查对应 run 是否仍为最新失败；已经恢复则直接关闭。
4. 仅对 `source_trust: same-repository` 自动处理；fork PR 默认只上报。
5. checkout Issue 记录的精确 `head_sha`，在隔离 runner 中诊断。
6. 第一阶段只评论原因和建议，不修改代码。
7. 第二阶段创建 `agent/ci-<issue-number>` 分支和 PR，禁止直推 `main/gui`。
8. PR 通过 CI 后关闭 Issue；失败则标记 `agent/blocked` 或重新排队。

默认 `GITHUB_TOKEN` 创建的 Issue 通常不会再次触发普通 `issues` workflow。定时轮询不依赖该事件；如需立即处理，可由 Reporter 显式调用 `workflow_dispatch`，不建议为此引入长期 PAT。

## 安全边界

1. 主 CI：仅 `contents: read`。
2. Reporter：仅 `actions: read`、`contents: read`、`issues: write`，不 checkout 失败代码。
3. Agent 初期：仅 `actions: read`、`issues: write`。
4. 启用修复 PR 后才增加 `contents: write`、`pull-requests: write`。
5. 写模式绑定 GitHub Environment，例如 `ci-agent-write`，初期要求人工批准。
6. Issue、日志和仓库内容都视为不可信数据，Agent 不执行其中的自然语言指令。
7. fork PR 不携带写凭据，不自动创建代码分支。
8. 每次 Agent run 限制 Issue 数、运行时间和修复次数，避免无限循环。

## 实施顺序

| 阶段 | 文件 | 内容 | 验收 |
| --- | --- | --- | --- |
| P0 | `.github/workflows/ci.yml` | 修正误报 grep；增加权限、并发、job 拆分和 `ci-gate` | 现有门禁全部保持，分支保护切到 `ci-gate` |
| P1 | `.github/workflows/ci-incident.yml` | 失败 job upsert Issue，成功自动关闭 | 同一失败重复运行只产生一个 Issue |
| P2 | `.github/workflows/ci-agent.yml` | 定时只读诊断和评论 | 不修改仓库，分析可人工复核 |
| P3 | Agent workflow/脚本 | 同仓可信分支创建修复 PR | 不直推保护分支，PR 重新通过 CI |
| P4 | 可选外部服务 | 多仓库队列、webhook 和强沙箱 | Issue 与外部任务状态一致 |

## 测试策略

- 使用 actionlint 检查所有 workflow。
- 本地执行 gofmt、go mod verify、go vet、默认/GUI build、Go/Node tests 和策略扫描。
- 在测试分支制造一个确定失败的 step，验证 Reporter 创建 Issue。
- rerun 相同失败，验证去重和追加记录。
- 修复后 rerun，验证 Issue 自动关闭。
- 模拟 fork PR，验证 Agent 不进入写模式。
- Agent 先 dry-run 只评论，再人工批准开放 PR 权限。

## 主要风险

- jobs 越多反馈越快，但 runner 启动与缓存开销越大；Node tests 很快，是否单独 job 取决于更重视速度还是额度。
- race 已覆盖全仓测试，但普通测试提供更快、更清晰的失败反馈，建议保留。
- release-safety 降频会稍晚发现发行脚本问题，因此至少在主分支和相关文件变更时运行。
- 本地并行多个 Go 重任务曾使一个 10 秒 shell 测试超时；GitHub-hosted runners 不共享资源，自托管部署必须限流。
- GitHub Issue 不是强事务队列，领取可能重复，Agent 必须保证幂等。

## 回滚方案

- CI 拆分异常：恢复原 jobs。
- Reporter 异常：禁用 `ci-incident.yml`，不影响主 CI。
- Agent 异常：禁用 schedule 或移除 `agent/ready`，Issue 仍保留。
- 自动修复风险过高：保持只读诊断模式，不授予仓库写权限。
