# DS-A2A goal 域 AB 链路冒烟报告（三等级题：数学/代码/政治）

> 日期：2026-09-07 · 套件：`tmp/goal-dsa2a-smoke`（go test）
> 配套：`docs/2026-09-07-goal-domain-techleader/techleader-mvp.md`、goal 包
> `advisor.go`/`techleader.go`/`gate.go`（DS-A2A 治理，未提交工作树切片）、
> `docs/2026-09-07-seele-a2a-framework-req/ds-a2a-protocol.md`。
> 政治内容分析（本冒烟之外的第三级真实内容）见
> `docs/2026-09-07-taiwan-strait-assessment/taiwan-strait-open-source-assessment.md`。

## 1. 被测对象与装配

| 链路 | 装配 | 语义 |
|---|---|---|
| **A（a = 无 a2agoal）** | `goal.NewServer(Controller)`，无 Supervisor/TechLeader | EXEC(a) 直连收口（Part I）；`goal_tl_*` RPC 显式拒绝——治理缺席不静默 |
| **B（b = 有 a2agoal）** | `+ Supervisor(Enabled, EvalWindow=0)` + 确定性 subject 规则评估器 | 打点/关键信号触发 b 回合：帧账本、corr 信封、缓存命中回升、B4 缺席矩阵 |

同一批「三等级」题（高考导数恒成立 / 回文实现+测试 / 台海局势专业研判）经
goal Headless HTTP RPC（`goal_begin/update/finish/status` + `goal_tl_*/propose/prescreen`）全流程驱动，
与真实接线位（未来 `gui/headless.go` dispatch 增行 `goal.<method>`）同契约。
评估器为确定性 policy stub（非真实 LLM）→ 断言可复现；真实 LLM 端到端需 P0 接线 + API 授权，属后续。

## 2. 结果矩阵（本机 `go test ./tmp/goal-dsa2a-smoke -v -count=1`）

| 科目 | 链路 | wall | rounds | frames | corr 指令 | cache_hit% | 收口 | 检查通过 |
|---|---|---|---|---|---|---|---|---|
| math（导数恒成立 a≤0） | A | 12ms | 0 | 0 | 0 | – | completed(direct_finish) | 4/4 |
| code（palindrome + go test） | A | 3ms | 0 | 0 | 0 | – | completed(direct_finish) | 4/4 |
| politics（台海研判） | A | 6ms | 0 | 0 | 0 | – | completed(direct_finish) | 4/4 |
| math | B | 6ms | 2 | 3 | 2 | 54 | completed | 12/12 |
| code | B | 10ms | 4 | 7 | 4 | 68 | completed | 9/9 |
| politics | B | 14ms | 4 | 6 | 4 | 61 | completed | 12/12 |
| math-low-approval（低风险预筛代答） | B | 5ms | 3 | 3 | 3 | 66 | completed | 6/6 |
| politics-429（B4 缺席） | B | 5ms | 1 | 1 | 0 | – | escalate_then_recovered | 5/5 |

明细：`tmp/goal-dsa2a-smoke/reports/ab-links.jsonl`（逐场景 checks JSONL）。

## 3. 关键观测与结论

**A（无 a2agoal）——治理缺席是显式而非静默**
- 三科目全部直连收口（completed、无超时），业务在无治理下照常收敛（Part I 语义回归）。
- `goal_tl_snapshot` / `goal_propose_finish` RPC 返回 `techleader 未装配` 错误 → 调教面/前端可区分
  "没接 TL"与"TL 异常"，不会把缺席当成功。
- rounds/frames/corr 全 0 → 与 B 的差异即 a2agoal 的可观测事实。

**B（有 a2agoal）——治理可观测、可断言、a 永不等待 b**
- b 惰性 bind（锚点 goal.start），帧账本 ref_seq 严格单调；turn 跳帧后 `Behind>0` 可观测
  （math=1、code=2），on_eval 一次性追平（Behind 归 0）——"a 推进而 b 落后再同步"符合协议 §2 C3/C5。
- **缓存命中回升**：round2 起 LastCached>0，终态 HitRatio 54–68%（相邻回合公共前缀命中，协议 C4）。
- b→a 指令均带唯一 corr 信封（幂等审计），条数=回合数；收口后 b reap(reason=done)。
- **政治科目护栏**（与专题分析同口径）：high 风险预筛不代答（转人工，默认拒绝兜底不变）；
  绝对化结论（"必有一战/时间表"式）被 `verdict_not_done` 打回 → 条件化+依据+不确定度后才放行收口。
- **B4 铁律**：b 回合 429/超时 → `escalate_human`，goal 保持 active（a 不卡死）；恢复后 `RunEval`
  重建 b 正常出裁决，无孤儿态。

**竞态**：`go test -race ./tmp/goal-dsa2a-smoke -count=1` 通过（4.3s）；
goal 包单元套件 `go test -race ./application/core/goal -count=1` 通过（3.3s）。

## 4. 覆盖边界与残余风险

1. b 评估器为确定性 stub：内容质量（数学答案正误、政治观点质量）不在本冒烟判定范围；
   math 结论校验仅做框架性结构断言。真实内容见专题文档/人工复查。
2. 政治科目 B 链路演示的是**治理闸门**（条件化收口、越权转人工），结论文本是样例框架；
   真实研判结论（会不会打/策略/部署）见台海专题，未在本冒烟内由 LLM 生成。
3. 全部驱动为进程内 goal.Headless HTTP；`gui/headless.go` 生产二进制接线（dispatch 增行）仍是
   P0-wiring 待办，真实 LLM + 生产装配的端到端回放需在该接线完成后补跑。
4. 本套件用内存 Store + 确定性评估器 → 毫秒级、无外网依赖，可进 CI。

## 5. 复现

```powershell
go test ./tmp/goal-dsa2a-smoke -v -count=1
go test -race ./tmp/goal-dsa2a-smoke -count=1
```
