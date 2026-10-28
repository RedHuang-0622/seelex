# 前置审查报告

## 需求摘要

将对话区右侧用户输入索引重做为带连续刻度、鼠标跟随波浪变形和双层级摘要的导航轨；每个刻度显示高对比用户问题，以及低对比的该轮最后一条 LLM 回复开头。

## 影响文件清单

| 文件路径 | 修改类型 | 具体位置 | 修改原因 |
|---|---|---|---|
| `contract/protocol/session_input_index.go` | 修改 | `SessionInputIndexRow` | 增加该轮最后 LLM 回复的有界摘要字段，保持现有定位字段不变 |
| `middle/residency/inputindex.go` | 修改 | `BuildInputIndex` | 在构建用户输入刻度时收集下一条用户输入之前最后一条非空 assistant 回复 |
| `middle/residency/inputindex_test.go` | 修改 | `TestBuildInputIndexMarksLoadedSection` 附近 | 用红测钉住“最后一轮 LLM 胜出、空回复不覆盖、下一条用户输入不会串轮” |
| `application/assembly/integration/session_input_index_test.go` | 修改 | `TestSessionInputIndexCoversUnloadedEarlyRounds` | 验证 durable 全量索引实际下发 question + reply 双摘要 |
| `gui/frontend/dist/conversation-wheel.js` | 修改 | `normalizeInputIndex` / `inputDashes` / `createConversationWheel` | 消费 reply 摘要；用 `transform: scaleX()` 实现随指针位置衰减的刻度波浪 |
| `gui/frontend/dist/conversation-wheel.test.mjs` | 修改 | 归一化、签名、交互纯函数测试 | 固定双摘要、无回复回退和波浪强度函数 |
| `gui/frontend/dist/styles.css` | 修改 | 对话导航轮轴段 | 将“短条 + 窄提示”改为刻度轨与双层级摘要浮窗，只动画 `transform` / `opacity` |
| `middle/residency/README.md` | 修改 | `inputindex.go` 索引 | 同步唯一构建函数和数据流 |
| `application/session/query/README.md` | 修改 | Input Index 说明 | 同步下发字段 |
| `gui/README.md` | 修改 | SessionInputIndex 说明 | 同步 GUI wire |
| `gui/frontend/README.md` | 修改 | `conversation-wheel.js` 条目 | 同步前端交互、摘要和 reduced-motion 口径 |
| `docs/gui/CHANGELOG.md` | 修改 | 新增日期条目 | 记录 GUI 可见行为 |
| `docs/gui/FRONTEND-CONSTITUTION.md` | 修改 | 组件规则表 | 登记对话导航轨的指针跟随与双层级摘要约束 |
| `CHANGELOG.md` | 修改 | Unreleased / 最新条目 | 记录契约新增字段与导航改版 |

## 依赖分析

- 上游：`SessionInputIndexRow` 是 `middle/protocol/model` 的别名，Application、Bridge、前端都读同一个契约。
- 中游：`middle/residency.BuildInputIndex` 仍是唯一装配判据；`application/session/query` 不新增第二套摘要逻辑。
- 下游：GUI Bridge 原样透传新字段；`conversation-wheel.js` 负责展示，不改 Snapshot、定位键、回读协议或用户输入 `round` 语义。
- 兼容：新增 JSON 字段为 additive change；旧后端缺字段时前端回退为空回复摘要，旧前端忽略未知字段。

## 循环依赖检查

- [x] 确认无新增循环依赖
- [x] 契约层仍为零行为 DTO；领域判定继续留在 `middle/residency`
- [x] 前端不推导“哪条是最后 LLM”，只消费后端下发的有界摘要

## 风险预估

- 群聊轮次边界错误：中概率，中严重度。用“下一条有效用户输入之前最后一条非空 assistant”并加跨轮用例。
- 大索引逐指针样式更新造成卡顿：中概率，中严重度。只更新可见刻度 DOM，pointermove 经 `requestAnimationFrame` 合并，动画属性限定为 `transform`。
- 回复摘要无界或泄露正文：低概率，高严重度。沿用 rune 上限并单独设置较短回复摘要上限。
- 旧/流式会话暂无 assistant：高概率，低严重度。回复层留空，不伪造文本。

## 建议方案

1. 契约新增 `reply_summary` / `reply_chars`，保持原有 `summary` 为用户问题摘要。
2. `BuildInputIndex` 在同一遍扫描中维护当前用户刻度下标；遇到 assistant 非空正文就覆盖该轮回复摘要，遇到下一条有效用户输入即切换归属。
3. 前端用纯函数计算每个刻度到指针中心的距离和波浪强度；DOM 只写 CSS 自定义变量，由样式以 `scaleX()` 表现起伏。
4. 摘要浮窗分为问题与回复两层：问题用 `--text`，回复用 `--muted`；无回复时只保留问题。
5. 保持点击定位、未加载页回读、键盘导航、拖拽和 reduced-motion 语义不变。
