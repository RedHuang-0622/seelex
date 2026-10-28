# 下一轮前端动效改造提示词

> 状态：**已实施（2026-10-28）**。运行时落地与删除/合并证据见
> [`audit.md`](audit.md)；本文保留为当时的工作包提示词，不再代表待办。

历史用途：把 [Seelex 前端宪法 v1.0](../gui/FRONTEND-CONSTITUTION.md) 落到
`gui/frontend/dist`。下文保留原始任务边界，实施结果以 `audit.md` 为准。

---

请按照 `docs/gui/FRONTEND-CONSTITUTION.md` 更新 Seelex 前端加载动画与交互动效。目标是让状态语义贴近 Codex 类 Agent 工作台：状态文字优先、反馈就地发生、持续输出可见、静止页面不循环、用户输入可打断并收敛到稳定状态。

## 任务范围

1. 先审计 `gui/frontend/dist/styles.css`、`motion.js`、`components.js`、`conversation-view.js`、`chat-view.js`、`app.js` 及相关测试，列出所有 animation、transition、硬编码时长、布局属性动画、reduced-motion 分支和通用 spinner。
2. 建立统一 motion token：`--duration-micro/fast/normal/slow/slower`、`--ease-out/in/in-out/linear`，旧 token 如继续保留只能做 alias。
3. 把对话尾部通用 spinner 改为局部运行状态：可读的执行状态文字 + 克制的 activity 标记；100ms 内不闪现加载指示。
4. 把工具调用 `running` 的唯一加载动画改为 shimmer：周期 1.8s、linear、只扫工具卡内部 activity 层、使用 transform，不改变尺寸；success/error/cancelled 后立即停止；reduced-motion 改为静态活动线。
5. 实现流式输出 cadence，不得在每个 token 上挂 CSS 入场：
   - 持续流最多每 50ms 合并刷新一次；
   - 首个 chunk 在下一帧可见；
   - 进入“接收中”使用 100ms 滞回，退出使用 150ms 滞回；
   - terminal 事件先 flush 尾包，再进入稳定态；
   - 用户接管滚动后不抢回尾部；
   - 最终文本、key、revision 和持久化事实不变。
6. 交互过渡统一为短时、可中断、无过冲：按压 100ms，hover/focus/tooltip 150ms，展开/弹层/视图进入 250ms，退出优先 150ms；删除 `rotateX`、扫过卡片 `top` 的动画、非必要 `border-color` transition。
7. 删除或迁移 `width`、`height`、`top`、`left`、`margin`、`padding` 动画。分隔条拖动是直接手势，不算 transition，可保留跟手；点击收起面板不得动画网格宽度。
8. 补齐 reduced-motion 生成时约束：立即到最终状态、内容可见、焦点可用、工具状态与 stream 文本仍更新。
9. 更新 `gui/frontend/README.md` 与 `docs/gui/CHANGELOG.md`，说明新的状态语义、cadence 和验证命令。

## 实现约束

- 复用现有 `motion.js`、`components.js` 和权威状态词表，不新增第二套工具状态或流式状态判据。
- cadence 参数只在专用流式调度模块定义一次；CSS 不得再写任意毫秒值。
- shimmer 只允许用于工具 running 和结构已知的局部加载，不得扩散到普通卡片。
- 不修改 Snapshot、Event、Schema、持久化格式或业务状态所有权。
- 不改用户已有改动，不 commit，不 push，不运行 `make clean/release/rebuild-gui`。
- 每个动效就近写 `seelex-motion` 声明；无法满足时写明 owner、原因和移除条件，不可豁免项不得豁免。

## 先红后绿

先增加以下失败用例，再改实现：

1. `motion-contract.test.mjs`：token 值、白名单属性、stagger 40ms、序列不超过 500ms、禁止布局属性动画、禁止任意毫秒值。
2. `tool-running-motion.test.mjs`：running 工具有且只有一个 shimmer activity；状态文字包含工具名；success/error 后无 running activity。
3. `stream-cadence.test.mjs`：50ms 合并、首包下一帧、100ms 进入滞回、150ms 退出滞回、terminal 前尾包 flush、滚动接管不抢尾。
4. 扩展现有 `components.test.mjs`、`motion.test.mjs`、`conversation-view.test.mjs`，钉住 reduced-motion 和稳定 key。
5. 增加静态审计，允许登记存量债务但不得新增违规。

## 验证

```text
Get-ChildItem gui/frontend/dist -Filter *.js | ForEach-Object { node --check $_.FullName }
node --test gui/frontend/dist/*.test.mjs
go test ./gui -count=1
go test ./application/session/viewstate -count=1
git diff --check
```

如果改动跨到不同前端模块，再补对应包测试。Windows 本地 `CGO_ENABLED=0` 时不得声称 race 已验证。

## 交付格式

必须列出：

- 动作语义表到实现的映射；
- 修改文件与关键类型/函数；
- 删除或合并的重复路径（文件:行 -> 唯一位置）；
- 新增 token、cadence 常量与测试名；
- 红测失败证据、修改后结果；
- reduced-motion、aria-busy、焦点与流式尾包证据；
- 仍未迁移的存量动画和明确 owner；
- `git diff --check` 与测试结果。
