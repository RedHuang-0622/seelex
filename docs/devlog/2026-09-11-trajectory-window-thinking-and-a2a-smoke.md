# 2026-09-11 轨迹窗口与 thinking 补齐 + 缓存与 A2A 冒烟

> 日期: 2026-09-11 | 范围: `gui/frontend/dist`（trajectory/trajectory-view/app/styles）、
> 验证记录。用户四问：轨迹没加载完对话？thinking 为什么不在轨迹上？发送的上下文
> 与缓存命中如何？用 computer use 做 A2A 冒烟。

## 1. 轨迹"没加载完"＝可见窗口分页，不是丢数据

事实：`renderTrajectory` 的输入是 `snapshot.conversation`，即**当前已加载的可见
窗口**（`limits.history_window` = 200 条消息）。长会话早期回合并未加载，轨迹与
上下文轴自然只覆盖这一段。

修复：轨迹顶部新增窗口边界条（`renderTrajectoryWindowInfo`），显示
「已加载窗口 200 / 会话共 830 条消息 · 更早的回合尚未加载」并在 `has_more_history`
时给出「加载更早」按钮（复用会话分页入口 `LoadMoreHistory`，加载后轨迹与轴一起
重建）。用户因此不会把分页误读成丢数据，也能就地往前翻。

## 2. 工具步骤的 thinking 丢失

事实：`buildTrajectory` 只在 `llm` 记录上带 `reasoning`。而持久化（以及恢复路径）
里，"这一步在想什么"挂在**发起工具调用的那条消息**上（`assistant/tool_call` 行携带
`reasoning_content`），工具记录因此丢掉了推理；轨迹详情的 THINK 面板也只出现在
文本记录分支。

修复：工具记录带 `reasoning`，并抽出 `renderThinkPanel` 供工具与文本两条详情分支
共用——工具行现在是 THINK + IN/OUT 三段。

## 3. 上下文一致性与缓存命中（现状证据）

仓库已有确定性冒烟：`application/core/context_cache_smoke_test.go` 按生产装配路径
逐轮固化请求，再按最长公共前缀（LCP，原始字节 / 1024 块）估算命中。本次实测：

| 场景 | 结果 |
|---|---|
| 单会话 16 轮（每轮 +5.3k token） | 末轮 raw 96.3% / @1024 95.8%；turn≥4 raw 均值 90.0%；聚合 90.5% |
| 第 7 轮激活 Skill（尾部追加） | 前缀未动：该轮 raw 80.3% 后回到 92.1% 并继续爬升 |
| provider 前缀争用模型（K 会话 / 保留 L 个前缀） | K=1→99.3%；K=3,L=1→**49.6%**；K=4,L=1→33.1% |

真实账单口径（`docs/test/REPORT-perf-latest.md`）：输入命中缓存 97.6%。结论：
**能命中**，且前缀是稳定的（Skill 激活走尾部追加、不改前缀）；差距主要来自
「聚合口径 vs 末轮稳态」（几何上 N 轮会话聚合率 ≈ (N−1)/(N+1)）与多会话热切换
争用 provider 的前缀缓存槽。调研稿列出的三个结构性漏点在本次未改：
记忆块按当前查询重算且放在累积上下文之前、工具结果上限偏高、GUI 多会话热切换。

## 4. A2A 冒烟（computer use 真机）

过程：打开会话 → 右栏「状态 → AGENT TEAM」→ 点「装配 goal-a2a」。

结果（正常项）：

- 装配成功：面板徽标 0 → 3，出现「goal-a2a 已装配」，工作顺序 `1 user / 2 main /
  3 tl`，成员表与 floor 标记渲染；
- 幂等提示正常（重复装配不新建第二个角色会话）；
- 落盘可核对：`team/roles.json`（team_kind=goal-a2a）与 `lifecycle.json` 的
  顺序头在同一秒写入。

发现并修复的问题：**右栏拖窄后团队面板被裁切**——页签逐字换行（"工作台"竖排）、
`floor` 标签与「删除」按钮被右边缘切掉。根因是 flex 子项默认 `min-width:auto`
不肯收缩，行宽超过面板。修复：团队面板各层 `min-width: 0` + 名字列
`flex:1 1 auto`，页签改 `nowrap` + 横向滚动。

## 验证

```text
node --test gui/frontend/dist/*.test.mjs   # 247 pass
go test ./application/core/ -run TestContextCacheSmoke -count=1   # PASS
go build ./...                             # OK
```

新增用例：`tool steps keep the reasoning that produced them`、
`trajectory window bar states which slice is loaded`。
