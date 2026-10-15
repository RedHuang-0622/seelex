# 缺失即状态：模块 head 的读面口径

> 状态：**已实现**（2026-10-15）。来源：`docs/2026-10-15-teamwork-defect-audit/README.md` D3。
> 作用面：`sessionstore` 的模块 head 读面，以及消费它的 `seelebridge` / `application` 读面。

## 1. 口径

**"还没有这份数据"是状态，"读不出来"才是错误。**

判据分两半：

- **缺失**（head 还没写过 / 记录还没产生）⇒ 读面给出**状态**，不是 IO 错误；
- **读不出来**（IO 失败、解码失败、schema 不符、checksum 不匹配）⇒ **仍然是错误**，不许被静默吞成"空"。

后半句与前半句一样重要：把"存档坏了"读成"没有存档"，就会拿一份新快照覆盖掉旧账本
（`application/core/goal/board_archive.go` 的"前值读不出来就不覆盖"正是这条）。

## 2. 为什么

真实现场（2026-10-15 审计 D3）：模型在一个还没有团队计划的会话里调 `team_items` /
`team_context`，收到

```text
open …\metadata\teamwork.json: The system cannot find the path specified.
```

于是它自行判定"这是错误，不是空结果"——一条内部路径噪声被读成了工具故障。同一个包里当时
有两种口径（`ReadTeamworkEvents` 早已是"缺失即空集"，`ReadTeamworkPlan` 原样上抛文件系统
错误），所以这不是"某处忘了兜"，而是**通则没有落在契约层**：谁新写一个读面就可能再犯一次。

## 3. 两种合法表达（消费方两种都要认）

| 档 | 形状 | 用在哪 | 为什么是这一档 |
|---|---|---|---|
| ① 空值档 | **零值 + nil** | 模块 head 的默认口径：`ReadTeamworkPlan`、`ReadTeamworkEvents`、`ReadGoalBoard`、`ReadTeamBoard`、`ReadToolRecordSnapshot`、`ListNodeSessions` | 上层直接把零值当"还没有"用，不必逐处记得映射哨兵值——哨兵值最容易被漏处理，那正是 D3 的现场 |
| ② 哨兵档 | **明确哨兵错误**（`fs.ErrNotExist` / `ErrNodeSessionNotFound`），调用方**在边界处一次性翻成状态** | 按显式 id 的查找（`ReadNodeSession(subSessionID)`）；压缩链内部状态（`readCheckpointLayout` 的"还没有续跑快照"） | 调用方点名了一个**应当存在**的东西 ⇒ "它不在"是错误；压缩链还要区分"没有快照"（从头跑）与"快照读坏了"（不覆盖） |
| ③ 退役档 | **恒返回** `fs.ErrNotExist` | `ReadState`（`state.json` 停读，D9/S20） | 它不是"缺失"，而是"这个面已经不存在"——语义与①②都不同，混进去会让人以为"再写一次就有了" |

消费方的义务：①②两种表达都要能认（实现迁移期两种都可能出现）。例：
`application/core/goal/board_archive.go` 既把零值当"首次写"，也把 `fs.ErrNotExist` 当同一状态；
其余错误一律"不覆盖"。

## 4. 不适用（边界）

- **按 ref 的查找**：`ReadToolResult(resultRef)`、`ReadMedia(ref)`——调用方点名了一个应当存在的东西，缺失即错误。
- **写面**：需要"已有计划/已有团队"的写面自己判空并给**领域文案**（`seelebridge/teamwork` 的
  `requireTeam`），判据不在存储层复制（`TeamworkPlan.IsEmpty` 是共享事实，不是第二份判定）。
- **模型面文案**：读面给状态之后，工具层负责把它说成人话（"本会话还没有团队计划——先用 team_plan…"）。

## 5. 门禁

`sessionstore/absent_state_reads_test.go` 把上面三档**逐条登记**：①档断言"零值 + nil"，
②③档断言各自的哨兵值。**新增 head 读面必须进这张表**——没登记的面就是下一个把"还没有"
报成 IO 错误的面。
