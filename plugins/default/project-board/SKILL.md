---
description: 项目看板（Project Board）按需读取：会话的任务行（worktable）与里程碑/工作项（teamwork）两投影；用 project_board_read 读，注意它是派生数据不是事实源
---

# 项目看板（Project Board）：按需读一份**派生投影**

看板是**派生数据**：把已存在的工作表格投影（todo/task 行）与 teamwork 计划/运行态
（里程碑、工作项）按**会话**落成追加式 JSONL。它可删可重建，**不是事实源**——
任何判定（谁在跑、做完没有、谁是 leader）一律读活体与领域事实，不读这两份文件。

## 什么时候读

- 用户问"这个会话/这个项目现在什么状态""我们做到哪了"；
- 你要**承接另一个会话**的活（它列过 todo / 跑过团队编排），需要先看清它在册的任务与里程碑；
- 团队收口后要回看：谁在哪个里程碑下做过哪几件（`milestone_item` 行带 `role` / `session_id`）。

**不要**把它当上下文注入源：它是按需读的一层，不进 system/项目前缀，也不进请求尾部。

## 怎么读（必须用专用工具）

`project_board_read` —— 只读、有界、按会话/项目聚合：

| 参数 | 说明 |
|---|---|
| `session` | 会话 ID（它就是落点目录名）；缺省 = 当前会话；`all` = 本项目内跨会话聚合（有界） |
| `file` | `worktable`（任务行）/ `teamwork`（里程碑 + 工作项）；缺省两份都读 |
| `project_root` | 项目根目录；缺省 = 当前会话所属工作区的根（看板落点 = `<project_root>/.seelex/board/<会话 ID>/`） |
| `limit` | 行数上限（缺省 100，硬上限 500；超限显式报错，触顶时返回 `truncated: true`） |

落点在**项目目录**里，且**按会话分目录**：`<项目根>/.seelex/board/<会话 ID>/{worktable,teamwork}.jsonl`
（人读件是看板根下的 `BOARD.md`，全项目汇总）。但 `read_file` / `grep_search` / `glob`
**依然读不到**它——Seelex 的工作区读面把
`.seelex` 整个列为忽略目录（默认数据根常同名，整体放开会把 `lock.owner`、会话正文一起放出来），
所以这条目录只有两把读法：本工具（有界 + 按会话归属），或你**自己**在终端里直接读
`BOARD.md`（外部 agent 如 codex / claude code 走的就是这条路）。

## 读到的形状

每一行是一条投影事实的**信封**：`kind` / `key` / `at` / `scope` / `payload`。

- `kind=work`：一条 todo/task（`payload`: `task` / `kind` / `status` / `assignee` /
  `dependencies` / `batch`）。注意 `plan` 节点与 `subagent` 行**不在**这份投影里
  （它们各有自己的读面：`read_plan` 与子代理树）。
- `kind=milestone`：里程碑（`id` / `name` / `depends_on`（里程碑之间是**屏障**）/
  `required` / `status` / `content` / `plan_version` / `team_state`）。
- `kind=milestone_item`：里程碑里的工作项（`id` / `milestone` / `role` / `depends_on`
  （里程碑内 DAG）/ `status` / `session_id`）。

同一个 `key` 只保留**最新**一行（追加流折叠读）；一行 `payload.removed = true` 表示那个
key 已经消失（被删掉了）。

## 能力开关

这套落盘与读取面由 `limits.project_board.enabled` 控制（出厂档**显式打开**；
置 `false` 即整块收回）。关着时：

- `project_board_read` **不存在**（不是"存在但报错"）；
- 一个投影文件都不会产生。

所以在这个技能里读到 `tool not found`，先去看配置开关，而不是去猜路径。

## 硬纪律

1. **不判定**：`status` 是映射来的，任何"该不该重派/能不能收口"的判断回活体与领域事实；
2. **不引用为输入**：投影被篡改不影响任何活体与领域判定（文件只是派生件）；
3. **可提交**：行内**不写** `scope.project`（`ws-<nano>` 是机器本地 ID），落点在
   `<项目根>/.seelex/` 而该目录在 `.gitignore` 的运行时数据段里，`BOARD.md`
   也不落 `at`/绝对路径/用户名 ⇒ 派生件既不入库也不泄漏本机标识。
