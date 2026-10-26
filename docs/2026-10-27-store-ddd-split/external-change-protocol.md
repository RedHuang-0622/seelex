# 外部依赖批次变更与回滚协议

日期：2026-10-27
状态：**强制协议；适用于后续所有 `store` DDD 拆分里程碑**
上位方案：[`plan.md`](plan.md)
执行计划：[`milestones.md`](milestones.md)
开发提示词：[`next-session-prompt.md`](next-session-prompt.md)

## 1. 目的

领域拆分最危险的变化不是新增包，而是同时修改大量外部消费方。
本协议要求把这类变化当作一个可审计、可一次性执行、可恢复的批次：

```text
扫描外部依赖
  → 记录文件与双向依赖
  → 创建专用 branch/worktree
  → checkpoint commit
  → 脚本 dry-run
  → 一波流应用
  → 编译与局部验证
  → 批次 commit
  → 全量门禁
  → 失败则 revert 批次
```

本文件是外部依赖清单格式、脚本要求与 checkpoint 回滚的唯一事实源。
其他文档只引用，不各自维护第二套流程。

## 2. 触发条件

任一条件成立，必须执行本协议：

1. 修改当前里程碑领域目录之外的生产文件或测试文件。
2. 修改 `contract/dto`、`contract/protocol` 中已被外部包消费的类型或 JSON 字段。
3. 修改根 `store` 的导出类型、方法、常量、接口或构造方式。
4. 需要批量替换 import、类型名、方法签名、构造调用或测试替身。
5. 删除、移动、拆分别名后，外部包需要同步切换。

仅修改当前领域新包内部文件，且外部调用点、导出面和 contract 均不变时，
不强制走脚本批次，但仍需正常单测和依赖门禁。

### 2.1 Worktree 硬门禁

外部依赖批量脚本**禁止在用户当前主工作区运行**。每次外部批次必须：

1. 创建专用 branch，名称使用 `codex/store-ddd-<milestone>`。
2. 在独立 worktree 中完成清单、checkpoint、脚本、验证和回滚。
3. 保持原工作区不被脚本写入；原工作区中的无关改动不进入该 worktree。
4. 如果主工作区包含尚未进入 base/checkpoint 的本任务准备改动，
   先做任务范围的提交，再从该提交创建 worktree；禁止手工复制、stash 或夹带无关文件。
5. 无法创建独立 worktree 或无法证明分支隔离时，停止批次，不降级为原地脚本。

worktree 必须从确定的 base commit 创建；准备清单和脚本后，在该 worktree 分支上创建
checkpoint commit，后续脚本以它作为回滚基线。所有批次 commit 也在该 worktree 分支上完成，
未经 review 不合并、不 push。

## 3. 外部影响清单

### 3.1 落点

每个里程碑一份：

```text
docs/2026-10-27-store-ddd-split/impact/<milestone>-external-deps.json
docs/2026-10-27-store-ddd-split/impact/<milestone>-external-deps.md
```

JSON 是脚本输入和审计事实；Markdown 是人类审查摘要。
两份文件必须在迁移脚本运行前完成并提交到 checkpoint。

### 3.2 最小 JSON 形状

```json
{
  "schema_version": 1,
  "milestone": "M1",
  "execution": {
    "worktree_branch": "codex/store-ddd-M1",
    "base_commit": "<base-sha>",
    "checkpoint_commit": "<checkpoint-sha>"
  },
  "checkpoint": {
    "commit": "<checkpoint-sha>",
    "created_at": "2026-10-27T00:00:00Z"
  },
  "providers": [
    {
      "package": "github.com/RedHuang-0622/seelex/store",
      "symbols": ["ProjectBoardRow"],
      "change": "alias -> contract/dto"
    }
  ],
  "files": [
    {
      "path": "middle/view/board/read.go",
      "package": "github.com/RedHuang-0622/seelex/middle/view/board",
      "side": "consumer",
      "sha256": "<pre-change-sha256>",
      "depends_on": [
        {
          "package": "github.com/RedHuang-0622/seelex/store",
          "symbols": ["ProjectBoardRow"]
        }
      ],
      "depended_on_by": [
        {
          "package": "github.com/RedHuang-0622/seelex/wiring/registry",
          "reason": "uses board read result"
        }
      ],
      "operations": [
        {
          "kind": "replace_import",
          "from": "github.com/RedHuang-0622/seelex/store",
          "to": "github.com/RedHuang-0622/seelex/contract/dto"
        }
      ],
      "verify_packages": [
        "github.com/RedHuang-0622/seelex/middle/view/board"
      ]
    }
  ],
  "summary": {
    "production_files": 0,
    "test_files": 0,
    "packages": 0,
    "direct_edges": 0,
    "reverse_edges": 0
  }
}
```

### 3.3 清单要求

- 路径必须是仓库相对路径，不得写本机绝对路径。
- 每个文件记录迁移前 SHA-256，脚本据此拒绝“清单过期”的执行。
- `depends_on` 记录该文件依赖的包与消费符号。
- `depended_on_by` 记录反向依赖者和原因；没有反向依赖时写空数组并说明。
- 生产文件与测试文件分开统计。
- 清单只包含本批次允许修改的文件。脚本发现清单外目标必须失败。
- 清单不得包含 `.seelex/`、`dist/`、`config/`、媒体、会话或其它用户数据。
- 清单需要人工 review 依赖方向，不能只按文本搜索 `store.` 命中判断。

## 4. 工作区前置检查

### 4.1 必须先检查

1. `git status --short`
2. 清单目标的当前 diff 和未跟踪文件
3. 当前分支和 HEAD
4. 是否有并行 agent 正在修改同一目标
5. 当前 seelex 进程是否在运行
6. 当前路径是否处于专用 worktree，`git rev-parse --show-toplevel` 是否为预期根

### 4.2 冲突处理

- 清单内目标有任务外的未提交改动：停止，不得覆盖。
- 当前工作树混有其它迁移：不得继续；先在专用 worktree/branch 重新开始。
- 不得自动 stash、reset、checkout 或清理其它 agent 的改动。
- 不得把无关改动包含进 checkpoint commit。

## 5. Checkpoint commit

脚本运行前必须创建本地 checkpoint：

```text
chore(store-ddd): checkpoint before <milestone> external batch
```

要求：

- checkpoint 只包含本里程碑已确认的代码、测试、影响清单和脚本。
- checkpoint 必须在专用 worktree 的分支上创建；主工作区不作为脚本回滚基线。
- 不 push。
- 记录 `git rev-parse HEAD`，写入影响清单和最终报告。
- checkpoint 必须可编译；无法编译时不得运行批量脚本。
- 若当前改动无法安全隔离，停止并请求用户处理，不执行脚本。

checkpoint 的作用：

- 提供确定的迁移前代码基线；
- 进程中断后用于重建/对照；
- 防止批次失败后无法判断原始内容；
- 让后续批次 commit 的 revert 保持清晰。

## 6. 一次性迁移脚本

### 6.1 落点与形式

脚本必须落盘，禁止通过 shell heredoc、管道内联或含中文的内联片段执行。

建议落点：

```text
scripts/store-ddd/<milestone>-external-migrate.py
```

脚本与本批次绑定，可保留为审计证据，不作为通用代码生成器。

### 6.2 必需能力

脚本至少支持：

```text
--manifest <path>
--checkpoint <sha>
--worktree <path>
--branch <name>
--dry-run
--apply
--rollback <journal>
```

脚本必须：

1. 验证 checkpoint 与 manifest 一致。
2. 验证 `git rev-parse --show-toplevel` 等于 `--worktree`。
3. 验证当前 branch 等于 `--branch` 且以 `codex/store-ddd-` 开头。
4. 验证目标文件存在且 SHA-256 与清单一致。
5. 拒绝清单外文件。
6. dry-run 只输出计划和 diff 摘要，不写文件。
7. apply 前把全部改动计算到内存，再统一写入。
8. 写文件使用临时文件 + rename，避免半文件。
9. 任一操作失败时恢复本次已触碰的全部文件。
10. 在系统临时目录写入 journal：worktree、branch、checkpoint、目标、原/新哈希、
    备份和失败原因。
11. 不访问网络，不读取 secrets/config，不触碰用户数据。
12. 对 Go 文件在成功后只对变更集合运行 gofmt，禁止全仓格式化。

### 6.3 脚本失败

脚本内失败：

1. 立即用本次备份恢复已触碰文件。
2. 验证恢复后的文件哈希等于迁移前哈希。
3. 输出 journal 路径和失败目标。
4. 不提交批次。
5. 若恢复失败，停止并向用户报告；可从 checkpoint 生成反向补丁恢复目标文件。

进程被强制终止：

1. 用 journal 执行 `--rollback`。
2. 验证所有目标哈希。
3. 再用 checkpoint 对照 `git diff`，确认只留下任务外改动。
4. 禁止用 `git reset --hard` 清理现场。
5. 保留失败 worktree 和 journal，直到审查结论完成。

## 7. 成功后的提交与验证

### 7.1 批次 commit

脚本成功后：

1. 检查 diff 只包含 manifest 中文件。
2. 编译受影响包。
3. 运行清单中每个 `verify_packages` 的测试。
4. 提交本地批次 commit：

```text
refactor(store-ddd): migrate <milestone> external consumers
```

5. 不 push。

### 7.2 全量门禁失败

若批次 commit 后全量门禁失败：

1. 保存失败命令和日志摘要。
2. 对批次 commit 执行 `git revert <batch-commit>`。
3. checkpoint 保持在历史中。
4. 不恢复或清理用户数据。
5. 修复后在新的 checkpoint 上重新执行整套流程。
6. revert 和复跑均留在专用 worktree；是否归档 worktree 由主会话在确认证据后决定。

### 7.3 部分成功禁止

不允许：

- 只改一部分外部包后留下不可编译状态；
- 用类型 alias 掩盖旧身份但清单未登记；
- 批次失败后手工保留一半改动；
- 把新脚本失败的回滚和业务数据回滚混为一谈。

## 8. 审查清单

- 清单是否列出所有直接依赖和反向依赖。
- 是否在 `codex/store-ddd-<milestone>` 专用 worktree 中执行。
- 依赖方向是否与新领域架构一致。
- checkpoint 是否漏带本任务文件或混入无关改动。
- 脚本是否只处理清单文件并校验哈希。
- dry-run 与 apply 目标集合是否一致。
- 脚本失败是否真实演练过恢复。
- 批次 commit 是否只包含预期文件。
- `git revert` 后能否恢复上一门禁状态。
- 回滚是否完全不触碰 `.seelex`、`dist`、配置和用户 fixture。
- 是否仍只有一份业务实现。
