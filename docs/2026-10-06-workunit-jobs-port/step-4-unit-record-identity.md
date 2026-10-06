# 步骤④ 落地记录：记录快照的**身份** + 恢复链分策略 + 写侧责任链

- 用户口径（2026-10-06）：teammate / subagent "做出了一些共性的 workunit 之后"剩下的两个接缝
  ——① 记录侧现场只写 Path/Branch（收尾合不回主分支）；② 恢复侧不区分记录属于哪条链
  （teammate 单元记录长成子代理节点）。解法按用户给的方向落地：
  - **快照记身份**：`type`（subagent/teammate）、`plugin`（teammate 独有）、`system_prompt`
    （teammate 可选）、`prompt`、`worktree`、`sessionid` 都写进记录快照；
  - **恢复链走策略模式**（两条链要读的东西、要恢复的内容确实不同）；
  - **写存储走责任链**（teammate 是 subagent 的纯增幅 = add but not modify，因此在同一条链上
    再包一环，不另立第二条写路径）。
- 前一步的审计与现网读数：`step-3-audit.md` §2 相邻事实、§9 仍开放 ①②⑥。

## 1. 改了什么（按层）

| 层 | 文件 | 改动 |
|---|---|---|
| 契约 | `application/contract/dto/worktree.go` | `NodeWorktreeInfo` 补 `BaseCommit`（四栏才是收尾要用的全部事实） |
| 存储 | `sessionstore/node_session_store.go` | 新增 `NodeUnitRecord`（Kind/Role/Plugins/SystemPrompt）+ `NodeSessionRecord.Unit`；`NodeWorktreeRecord` 的注释写明四栏缺一不可。**prompt 不另立一栏**：它就是既有的 `Goal`（同一份事实不存两遍） |
| 存储（新） | `sessionstore/unit_record_writer.go` | 写侧**责任链**：`NodeSessionRecordWriter` + `With`（在外层再包一环，新环先跑）+ `Store()`（读面仍走末端） |
| 会话面 | `seelebridge/session/subagent_sessions.go` | 持**链**而不是裸存储；`SubagentUnitRecordLink`（子代理那一环：**只填空缺**）；`Configure`/`WithNodeSessionStore` 收链 |
| 宿主 | `seelebridge/runtime.go`、`runtime_subagent_recovery.go` | 装配时建链（末端 = 存储，第一环 = 子代理身份）；恢复改为 `restoreUnitRecords` 分派 |
| 宿主（新） | `seelebridge/runtime_unit_recovery.go` | 恢复链**策略面**：`subagentRecovery`（详情面 + 子代理树 + 现场登记）/ `teamUnitRecovery`（**只有现场登记**，**不进子代理树**）+ `unitKindOf`（唯一转换点）+ `restoreUnitRecords`（按身份切批） |
| 团队面 | `seelebridge/workunit_team_records.go`、`workunit_team.go`、`runtime_teamwork.go` | teammate 记录 = 子代理链**外面再包一环**（`teamUnitUnitRecordLink` 写 Kind/Role/Plugins/SystemPrompt）；`teamUnitWorktreeRecord` 取**四栏**；记录身份里补装配层算出的系统提示（开跑之前写） |
| 门禁 | `e2e/subagent_status_vocabulary_gate_test.go` | 新增"单元身份"格（写方/读方/取值面），词表只有契约一份 `workunit.Kind` |

**分工一句话**：写侧是**责任链**（同一条链上一环一环增补，teammate = 子代理链 + 一环），
恢复侧是**策略**（按记录自己写的身份分派），两者共用同一格身份——`NodeUnitRecord`。

## 2. 红 → 绿（用例先红后绿，原文摘录）

用例：`seelebridge/runtime_unit_record_identity_test.go`（真 git 仓 + 真 JSON 存储 + 两次装配）。

```
# 红（改动前）
runtime_unit_record_identity_test.go:117: 记录侧缺 MainBranch（这次合回哪条分支没有来源）：{Path:...\003-seelex-exec-wi-1 Branch:seelex/exec-wi-1 MainBranch: BaseCommit:}
runtime_unit_record_identity_test.go:120: 记录侧缺 BaseCommit（变基与提交判定的基线没有来源）：…
runtime_unit_record_identity_test.go:138: teammate 单元记录被当成子代理节点恢复了（工作表格上那条 `subagent:exec-wi-1 interrupted` 假行）：树上节点=[main exec-wi-1]
runtime_unit_record_identity_test.go:148: 记录侧恢复出来的现场缺 MainBranch：Info={Path:… Branch:seelex/exec-wi-1 MainBranch:}
runtime_unit_record_identity_test.go:218: 老 teammate 记录（快照无身份）仍被当成子代理节点恢复：树上节点=[main exec-wi-legacy]
FAIL  github.com/RedHuang-0622/seelex/seelebridge  2.340s

# 绿（改动后）
seelebridge: 恢复 teammate 记录 1 条（会话 sess-1）
seelebridge: 恢复 teammate 记录 1 条（会话 sess-legacy）
seelebridge: 认领团队现场 1 个（来源：团队计划 + 绑定账本）
--- PASS: TestTeamUnitRecordRestoresAsTeamworkSceneNotSubagentNode (0.98s)
--- PASS: TestLegacyTeamUnitRecordWithoutIdentityIsClassifiedByTeamScene (0.93s)
```

一条**读数教训**（第一版用例自己判错了）：`SubagentTree.Projection()` 是"合成根 `main` +
递归子节点"，第一版只收顶层 id，于是断言恒真（门禁假绿）。改成递归整棵树才抓到真现象
——**判据写错比实现写错更难看出来**。

## 3. 新增/改动的用例

| 用例 | 钉住什么 |
|---|---|
| `TestTeamUnitRecordRestoresAsTeamworkSceneNotSubagentNode` | 产品写点落下的 teammate 记录：重启后①**不是**子代理树节点 ②现场在册且 `MainBranch` 有来源 ③现场目录不被 Prune 清掉 |
| `TestLegacyTeamUnitRecordWithoutIdentityIsClassifiedByTeamScene` | 加固前的老记录（快照无身份）按**团队事实**（计划 + 账本名单）归 teammate，同样不长成子代理节点 |
| `sessionstore/unit_record_writer_test.go` | 写链：外层环先写、内层环只填空缺（不覆盖）、派生新链不动本链、未装末端**显式报错** |
| `e2e/subagent_status_vocabulary_gate_test.go`（新格） | "单元身份"这一格的字面量门禁（写方/读方/取值面） |

**既有用例只做机械适配**：本步**没有**改任何既有断言的期望值（`worktree_weak_registration_merge_test.go`
那条"缺栏登记必须显式发声"的用例仍然有效——它喂的是**手搓的弱记录**，而产品写点现在写四栏了）。

## 4. 冒烟读数（全部 leader 亲跑）

| 项 | 读数 | 原文 |
|---|---|---|
| `gofmt -l`（改动范围） | **空**（顺带把上一轮团队合并进来的 6 个未格式化文件补齐：`sessionstore/board_test.go`、`sessionstore/teamwork_items.go`、`seelebridge/runtime_teamwork_board*_test.go`、`seelebridge/teamwork/teamwork.go`、`application/core/teamwork_board_projection_test.go`） | — |
| `go build ./...` | exit 0 | — |
| 两张状态门禁（含新"单元身份"格 + 阴性对照） | `go test ./e2e/ -run 'TestSubagentStatusVocabularyGate|TestStateEnumNeverCastToString' -count=1` → **ok 5.548s** | — |
| 改动相关的端到端冒烟（真 git 仓 + 真 JSON 存储 + 两次装配/重启） | `go test ./seelebridge/ -run 'TestTeamUnitRecordRestoresAsTeamworkSceneNotSubagentNode|TestLegacyTeamUnitRecordWithoutIdentityIsClassifiedByTeamScene' -v -count=1` → **PASS · PASS**（0.98s / 0.93s） | `_logs/` |
| **全仓** `go test ./... -count=1` | 除一条**负载抖动**外全绿：`seelebridge/account` 的 `TestSlowHealthyStreamSurvivesIdleWatchdog` 在满并行负载下红（300ms 空闲看门狗窗口），该包单跑 `-count=3` → **3/3 PASS**，子树重跑 → **ok 3.755s**；其余含根包 headless 真实装配冒烟 **ok 73.181s** 全绿 | `_logs/unit_record_identity_smoke.txt` |
| **子树** `go test ./seelebridge/... -count=1`（改动最密的子树） | **33 行全 ok**（含 `seelebridge` 169.9s / `worktree` 131.6s / `session` / `teamwork` / `workunit` / `account`），零 FAIL | `_logs/unit_record_identity_seelebridge.txt` |
| 发行档构建 `scripts/build.ps1` | **exit 0 · build complete**（4 平台树 + 4 个归档：linux-amd64 / darwin-amd64 / darwin-arm64 / windows-amd64） | — |

## 5. 仍然开放（不在本步范围）

| # | 事 | 为什么停手 |
|---|---|---|
| 1 | #7 单读面：让**一条**读面同时给 teammate 会话正文 + worktree + 阶段 | 要动 wire（`dto.TeammateSessionLiveView` 的键表）与前端渲染；本步把"这些词从哪来"补齐了（记录快照里有状态词与现场四栏），wire 那一半仍属产品决策 |
| 2 | 跨进程无锁：收尾串行化只在**本进程**（`finishActor`） | **本步判定不做**：加第二道强制层要有真实双进程复现才能证伪，锁写坏了下场比没有锁更差（可能卡住合并）。判据与复现方案见 `step-3-audit.md` §9 ④ |
| 3 | U6 #6/#10（`running\|free` 归格、`"killed"` 与 `job_manage` 四 op） | 归格待定，登记在 `state-machine-inventory.md` §3 |
