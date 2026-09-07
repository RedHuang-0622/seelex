# 双会话不对称 A2A 协议（DS-A2A / 0.1）

> **状态：当前基线（权威协议，唯一真相源）。** 协议字段/schema 的变更必须版本化（0.1 → 0.2…）并同步
> `README.md` 状态矩阵；实现与前端一律以本文 + `ds-a2a-detailed-design.md` 为准。

> 位置：seelex 需求系列 `docs/2026-09-07-seele-a2a-framework-req/`。
> 一句话：**EXEC（执行/main，会话 a）与 ADVISOR（评审/TL，会话 b）各自维护独立会话上下文，**
> **经本协议把 a 的事件增量按需帧化同步给 b、把 b 的回合产物结构化回注 a；**
> **b 的上下文前缀稳定、只尾部追加 ⇒ provider 前缀缓存命中率高；a 永不因 b 缺席卡死。**
> 本文件只定义协议（信封/帧/时序/一致性/生命周期/错误），产品语义（goal、TL 判定）由上层注册。

---

## 1. 角色与术语

| 词 | 定义 |
|---|---|
| EXEC（a） | 执行会话。用户可见视图、全量转录、写工具、plan/task/goal 主状态；协议中唯一的 writer。 |
| ADVISOR（b） | 后台评估会话。独立完整会话、只读/无工具面；协议中只能产出建议帧，永不写 a。 |
| CONTROLLER（C） | 创建/绑定/回收 b 的运行时主体（goal 域 owner）。驱动同步与生命周期。 |
| 上下文 C_x(t) | 会话 x 在 t 时刻送给 LLM 的请求体 = 固定前缀 P_x ⊕ 历史 H_x(t)。 |
| P_x | 固定前缀：x 的角色 system、工具 schema、输出格式说明。**每会话恒定，不内联可变内容。** |
| seq | a 的事件日志全局单调号（a 是账本源）。 |
| 帧 frame | 同步单位：a 的一段事件（1..n 个 seq）打包成一段字节，尾部 append 到 b。 |
| 水位 s* | 帧/评估所基于的 a 事件水位（frame 带 `ref_seq=s*`）。 |
| corr | 关联 id：gate.request↔gate.verdict、advisory 幂等去重用。 |

---

## 2. 上下文与缓存命中模型

```
C_a(t) = P_a ⊕ H_a(t)            # EXEC：全量转录，每轮尾部增长（用户可见）
C_b(t) = P_b ⊕ F_b(0..k) ⊕ G_b(0..j)   # ADVISOR：帧历史 + 自身回合产物，均只尾部 append
```
缓存命中（provider prefix cache）：两次请求共享连续前缀 token ⇒ 第二次命中前缀，只计费新增段。
规则：
- **C1（前缀稳定）**：P_b 恒定；可变内容（goal 文本、时间戳等）绝不内联进 P_b，全部走帧/回合段。
- **C2（只尾部追加）**：b 的历史只允许 append，永不重写/重排/删除前缀段；压缩只影响旧段且保留前缀对齐。
- **C3（事务式评估）**：默认 MODE=on_eval——b 的一次 LLM 调用 = 先一次性 append 落后帧段，再追加回合提示；
  于是请求体 = 上次完整前缀（全命中）+ 一段新帧（仅此计费）。
- **C4（零增重估）**：无新帧的复核请求不 append ⇒ 请求体与上次全等 ⇒ 全命中（近似零成本节流重估）。
- **C5（跳帧自由）**：mirror 按策略抽帧（milestone/工具打点/压缩摘要），b 允许落后；帧带 `ref_seq` 保持因果。

观测字段：b 每次 LLM 调用记录 `usage.input_tokens` 与 provider 返回的缓存命中 token（如
`cached_input_tokens`），用于验证命中回升；协议要求这两值随回合事件上报。

---

## 3. 信封（单一 wire 格式）

```jsonc
{
  "v": 1,
  "id": "m-7c21",            // 消息唯一 id
  "ts": "2026-09-07T03:12:00Z",
  "from": "a",               // a | b | controller
  "to":   "b",
  "kind": "frame.append",    // 见 §4
  "seq":  0,                 // 仅 frame/ack 用：a 侧水位
  "ref_seq": 0,              // 载荷基于的 a 水位（frame/advisory/gate 用）
  "corr": "",                // gate 请求/应答 与 advisory 幂等
  "session": {"main": "sess-a", "peer": "sess-b", "goal": "goal-1"},
  "payload": { }             // schema 由 kind 决定；帧载荷结构见 §4
}
```
投递语义：进程内 = 队列/事件流投递；跨进程 = 同一 JSON over 任意 transport。**不区分**——协议相同，
进程内是零拷贝特例。

---

## 4. 帧类型（a→b 同步内容；CONTROLLER 或 mirror 组件编码）

| kind | payload | 何时发 | 默认进 b？ |
|---|---|---|---|
| `peer.bind` | `{role:"advisor", mirror_mode:"on_eval", anchor_seq}` | goal 域创建 b 时 | — |
| `peer.unbind` | `{reason:"done|abort|429|killed"}` | b 生命周期终点 | — |
| `goal.start` | `{goal_id, statement, acceptance, budget}` | goal_begin | **锚点必进**（用户例中 b 的 5） |
| `goal.update` | `{goal_id, progress, pending}` | goal 状态变更 | 按策略 |
| `transcript.inc` | 增量消息段（用户输入/关键回复，受信帧） | 每轮 | 默认否（跳帧，见 C5） |
| `tool.checkpoint` | `{tool, summary, outcome}` | 里程碑工具调用后 | 按策略（推荐 yes） |
| `context.compacted` | `{summary, from_seq, to_seq}` | 压缩发生时 | **必进**（b 需知道 a 折叠了什么） |
| `approval.requested` | `{req_id, risk, tool, preview}` | ask_approve | 按策略 |
| `terminal.proposed` | `{goal_id, candidate:"finish", evidence}` | main 提议终态 | **必进**（触发 gate） |

## 5. b→a 载荷（建议帧；结构化，非自由文本）

| kind | payload | 语义 |
|---|---|---|
| `gate.verdict` | `{decision:"done"\|"not_done"\|"rework"\|"escalate", reason, ref_seq, corr}` | 终态裁决（响应 `terminal.proposed`/`gate.request`） |
| `advisory.send` | `{type:"directive"\|"evidence"\|"escalate", text, risk, ref_seq, corr}` | 纠偏/证据/升级 |
| `advisory.ack` | `{corr, adopted:true\|false, note}` | a 侧采纳回执（可选，审计用） |
| `error` | `{code, message, ref_seq?}` | 429/超时/坏 seq 等（见 §9） |

**约束**：b→a 任何载荷只进入 a 的"受信注入区"（渲染为系统级帧，不污染 a 转录主历史）；由
CONTROLLER 排空并注入（对应 seelex `injectPendingSubagentContextsFor` 挂点）。

---

## 6. 时序（= 用户例子的协议化）

```
t0  CONTROLLER: goal_begin → spawn b；C_b = P_b ⊕ [goal.start(5)]
    a 继续执行（其转录 1..4 为上下文既有内容；5 处 goal.start）
t1  触发水位到 → mirror 抽帧（on_eval 模式：落后帧一次性 append）
    C_b = P_b ⊕ [goal.start(5)] ⊕ [frame(ref s5)]      # 用户例 b: 1' 5 6(b)
    b 回合 → 输出 advisory（如"先做 x"）→ a 受信注入
    b 自回合段追加：C_b ⊕= [self(6_b)]                 # b 的 6(b)
t2  a 继续 → 6、7（a 侧）… 产生 tool.checkpoint/terminal.proposed 事件
    mirror 滞后（b 落后，不每步同步）→ C_b 暂时不变
    b 上下文稳定 ⇒ 期间任何零增复核全缓存命中（C4）
t3  下一次关键水位（如 terminal.proposed）→ mirror 拉新帧
    C_b = 旧前缀(全命中) ⊕ [frame(ref s7)]
    b gate 回合 → gate.verdict → a 裁决区
t4  goal 收敛/abort → CONTROLLER: peer.unbind{reason} → 停议程 → drain/丢弃 pending → reap b
    429/上游不可用 → peer.unbind{reason:"429"} → 同 §8
```

## 7. 一致性规则（同步语义）

1. **账本源单一**：seq 只由 a 事件日志产生；b 从不产生 a 的 seq。
2. **帧单调**：b 的帧段按 `ref_seq` 单调 append；`ref_seq` ≤ 已应用水位 → 幂等丢弃（`dup_frame` 记账）。
3. **按需同步（on_eval）**：b 回合前由 CONTROLLER 执行 `QueryRange(session_a, from=applied, to=now)`
   → 编码为帧段 → append → 回合；一次同步 + 一次 LLM 调用保持事务（C3）。
4. **因果**：b 任何输出携带 `ref_seq`（= 它评估所基于的水位）；a 侧 gate 只接受
   `ref_seq ≥ 裁决所需水位` 的 verdict，否则要求重估或按缺席处理。
5. **幂等**：帧靠 `ref_seq` 去重；advisory/gate 靠 `corr` 去重；重发安全。
6. **有界**：b 回合预算、注入队列（现 32）、帧段体积上限均为配置；超限丢弃最旧并记账，不阻塞。

## 8. 缺席与降级（B4）

| 情形 | 协议动作 | a 侧裁决默认 |
|---|---|---|
| b 不存在/未 bind | gate 请求就地判负 | escalate 或 allow（按风险策略，低放行/高人工） |
| 429 / provider 错误 / 超时 | `error{code:429\|timeout}` → unbind | 同上；a 进入 direct 模式继续执行 |
| `peer.unbind` 后残留 pending | CONTROLLER 显式 drain/丢弃并记账 | — |

**铁律**：a 的任何路径（含终态 gate）**不允许等待 b**；所有等待都有 deadline，超时 = §8 行。把这条
写成测试断言（模拟 429 下 goal 流仍收敛）。

## 9. b 生命周期状态机（CONTROLLER 为 owner）

```
detached → binding → bound(goal) ──► evaluating ──► advisory_pending ──┐
              ▲                     （水位触发,预算内）                    │
              │                                                        ▼
              └──────── 仍 bound（同 goal 内循环）◄──────────────────────┘
terminal: completed(goal done/abort → unbind+reap)
         evicted(429/error → unbind+reap)
         killed(会话关/超时 → reap)
```
- 每个 goal 域一个 b（干净基线）；`peer.unbind` 只删会话对象，b 回合/裁决事件已入审计账本。

## 10. 错误码

| code | 含义 | 处理 |
|---|---|---|
| `429` | provider 限流/配额 | unbind，a 走 §8 |
| `timeout` | b 回合/裁决超时 | 重试 1 次或按 §8 |
| `bad_seq` | 帧 ref_seq 乱序/跳跃（跳帧合法，回退非法） | 丢弃 + 记账 |
| `dup_frame` | 重复帧 | 幂等丢弃 |
| `unbound` | 消息发给未 bind 的 peer | 忽略 + 告警 |
| `schema_violation` | 载荷不符 kind schema | 拒绝 + 告警 |

## 11. 最小消息示例

```jsonc
// a→b：终态提议（必进帧）
{"v":1,"id":"m-1","from":"a","to":"b","kind":"terminal.proposed",
 "ref_seq":17,"session":{"main":"sess-a","peer":"sess-b","goal":"goal-1"},
 "payload":{"goal_id":"goal-1","candidate":"finish",
            "evidence":{"tests":"PASS","verification":"headless ok"}}}

// b→a：gate 裁决（corr 关联；ref_seq=已评估水位）
{"v":1,"id":"m-2","from":"b","to":"a","kind":"gate.verdict",
 "corr":"m-1","ref_seq":17,"session":{},
 "payload":{"decision":"not_done","reason":"缺负路径单测，补后再提","corr":"m-1"}}

// 生命周期
{"v":1,"id":"m-3","from":"controller","to":"b","kind":"peer.unbind",
 "payload":{"reason":"done"}}
```

## 12. 待确认（协议级参数，其余不阻塞）

1. mirror_mode 默认 **on_eval**（事务式、缓存最优）vs 连续推送（视图实时性优先）？
2. 帧跳变策略默认集：`goal.start / goal.update / tool.checkpoint / context.compacted / terminal.proposed / approval.requested` 进 b，
   `transcript.inc` 默认否（跳帧控量）——是否采纳？
3. `gate.verdict` 缺省缺席裁决 = low 放行 / high escalate（对齐审批预筛）？
