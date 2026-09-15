# 2026-09-15 · Agent Team 面板与左栏会话条目第二轮（去注释 / 浮层菜单 / 并库 / 串珠条）

这一轮只做一件事：把上一批"面板降噪"之后仍然刺眼的地方按用户口径再收一遍——
对话不靠颜色、会话条目紧凑、更多操作走浮层菜单、面板不留注解、员工库不再空壳、
团队库不再假装有内容、发言调度不摆表格。

## 一、对话：颜色高亮彻底去掉

上一批把"整块背景色"换成了"左侧 3px 状态条 + 发言人名"；用户这一轮明确
"message 的背景色（准确来说是左侧的颜色高亮）去掉"，于是 **状态条也删掉**：
`styles.css` §20 现在只剩两行发言人名着色（EXEC 冷钢蓝 / ADVISOR 暖黄）。
`components.js` 仍按 `role_name` 挂 `is-exec` / `is-advisor` 类（语义标记留着，
将来要按归属做别的呈现不必再改渲染件）。

保留：用户自己的输入气泡（`.message.user .message-body` 的 `--surface` 底）——
它是"这条是我说的"的功能区分，不是归属分区。若用户也要素底，删这段 CSS 即可。

## 二、左栏会话条目：紧凑 + ⋯ 浮层菜单

- 紧凑：行高 24px、内边距 `1px 4px`、分组间距 1px、状态 chip 9px（`--row-min-h`
  只管团队表格，不跟着一起缩）。
- ⋯ 常驻：去掉 hover 才显形的 `opacity: 0` 规则与"向右撑出三个按钮"的
  `session-more-actions`（连 CSS 一起删干净），改成一枚 18px 图标按钮。
- 点开是**浮层菜单**（Qoder 式省略号菜单）：`#session-menu` 挂在 `document.body`、
  `position: fixed`，所以不会被左栏滚动容器裁掉；下方放不下自动翻到上方，左右夹在
  视口内；三项带文字标签，删除用危险色。
- 分派只有一份：菜单项与行内动作带同一批 `data-*` 键，点击经
  `dispatchSessionListAction(dataset)`（`onSessionListClick` 也走它），
  `state.openSessionMenu` 仍是唯一开合状态，重绘后 `syncSessionMenu()` 重新贴位；
  滚动 / 缩放 / Esc / 点外部关闭。

## 三、Agent Team 面板：从"注解 + 整表动作"到"行内最小动作"

用户这一轮的口径是"不要注释、不要没用的表、操作要落在具体对象上"：

1. **栏头注解全删**（`teamRailHead` 去掉了 hint 参数）：`全局·跨会话 · 不依赖团队`、
   `N 个内置形态`、`拖拽行首手柄调整发言顺序`、`装配与编排`、`尚未装配团队`。
2. **删掉「默认顺序 vs 本会话」那张 项/值 表**（连 `data-team-default-order`
   按钮一起），Team 栏的装配参数也压成一行 chip（形态 / 顺序策略 / 发言权）。
3. **员工库 = 可用员工**：读侧合并 `employeePool(global, team)` =
   母本 `employees` ∪ 会话副本 `composition.employees` ∪ `TeamView.members`
   （按 `role_name` 去重、母本优先），每行标 `库` / `本会话`。
   母本为空但会话有人时，计数不再是 0，行上给一个「入库」
   （`AgentTeamSaveEmployee`，只写母本）；库里的行给「编辑 / ✕」。
4. **团队库只列用户团队**：团队名是按钮 → 打开"这一支"的团队面板（面板标题写明
   是哪一支，右上角 ✕）；行内只留「装配」与「✕」。内置形态退成表下一行 chip
   （点一下就地装配、当前形态禁用），不再作为库条目出现、不再有「存入库」。
5. **撤掉三个整表级写动作的 GUI 入口**：`存当前会话`（`AgentTeamSaveCurrentTeam`）、
   `入库当前会话`（`AgentTeamPublishToGlobal`）、`顺序设为默认`
   （`AgentTeamSetDefaultOrder`）。Bridge/Application 方法**保留**（headless 与工具面
   仍可用，`gui/README.md` 已注明）；GUI 里等价的能力改为行内动作 +
   团队面板的"从当前会话填充 + 保存团队"。
6. **团队面板的成员表**（`renderTeamMemberList`）：行序即发言顺序，行上字号 +
   身份 +「✕」，整行可拖拽调序；**可以从员工库把行拖进来**（落到某位成员上 =
   插到它之前，落到空表 = 追加）。这些改动都只动表单草稿，点「保存团队」才落
   `order_roles`（`agentTeamEntryFromForm` 按行序序列化）。
7. **拖拽补一条通道**：员工库的行拖到员工栏/顺序末尾落区时，若这人还不在会话里，
   先 `AgentTeamInstantiateRole` 落到会话（`agentTeamRolePayload` 取库里那一份配置），
   再 `AgentTeamSetOrder` 落到拖放位置；会话还没装配（没有顺序策略）时只入职、
   不硬塞空策略去写 lifecycle。
8. **发言调度改成顺序串珠条**（不再 项/值 表）：序号 + 身份，`发言中`（floor）与
   `下一个`（`schedule.next_role`）各占一档高亮，`unexecuted` 的角色虚线标
   "无执行者"；上方徽标是 `轮次 / 上限`，下方一行 meta 是 user 席位口径与收束原因。

### 群聊（酒馆）交互调研（子代理，只读）

来源：`docs.sillytavern.app/usage/core-concepts/groupchats/`、
`SillyTavern/SillyTavern@06bde939` 的 `public/scripts/group-chats.js` / `public/index.html` /
`public/css/rm-groups.css`。可用的事实：

- 发言顺序就是成员数组顺序（列表顺序），另有 Reply order 四个策略
  （Manual / Natural / List / Pooled），**没有**独立的 "random" 选项；
- 成员行的 enable/disable 是"临时静音"（写 `disabled_members`），不是删除；
- 组队排序是 ↑/↓ 按钮（原版没有拖拽排序），成员行没有 talkativeness 滑块；
- "下一个谁发言"是可选功能（`show_group_chat_queue`，默认关），开启后队首
  `.is_active`、其余 `.is_queued` 描边，位置徽标 `#n`；**没有大屏播报**；
- 聊天区靠消息对象里的 `force_avatar` / `ch_name` 标注发言人。

移植取舍：**抄它"位置 + 编号"的表达**（串珠条 + 序号 + 下一个高亮 + 轮次徽标），
**不抄**它的 toast 播报、hover 才出现的行内按钮，也不引入"随机顺序"这种它其实
没有的选项。窄栏（220–480px）下顺序用竖向/换行的胶囊链比表格更省宽。

## 四、验证

- `node --test gui/frontend/dist/*.test.mjs` → 303 / 303 通过（新增：员工库并库、
  `teamMemberNames` / `renderTeamMemberList`、面板注解缺席断言）。
- `go build ./...` 干净；`go test ./gui/... ./application/...` 全绿（
  `gui/bridge_test.go` 的嵌入前端契约断言同步改成"标题段 + ⋯ 浮层菜单""四块面板 +
  串珠条"，并把"不再有 `data-team-save-current` / `team-rail-hint`"写成回归断言）。
- 真机验收：用 `-store tmp/uicheck/...` 起了独立数据根的临时实例（不碰用户正在跑的
  那个），截图确认：会话条目紧凑 + ⋯ 常驻、点开是浮层菜单（置顶/分支/删除三项）、
  面板四块无注解、`员工库` 在只有会话员工时显示 1 人并标 `本会话` + 「入库」、
  团队库空态 + 内置 chip、员工栏三列表不再折字、发言调度串珠条
  （`1 USER 下一个` + 其余位次 + 席位口径 + "无执行者"标注）。验收后实例与临时
  目录已删除。

## 五、遗留与已知

- 面板的"员工库并库"是**展示层**合并：删除只对母本里的行开放（会话独有的行只能
  「入库」），两份事实的作用域边界没有被合并动作改变。
- 团队面板的成员表是草稿：不点「保存团队」就丢弃；`data-team-form-members` 文本域
  已被成员表取代（`gui/frontend/README.md` 已改）。
- 消息颜色改动是纯 CSS 删除，未做真机发消息验收（需要真实模型回合；
  临时实例没有发消息，避免无谓 API 调用）。用户自己跑一轮即可看到素底正文。
- `dist/seelex-gui-dev/seelex-gui.exe` 与 `dist/dev/seelex.exe` 由 post-commit 钩子
  重建；**用户当时正开着 GUI，占住了 exe，钩子会构建失败**（钩子容错，提交不受影响）。
  看到新界面需要先关掉 GUI 再跑 `scripts/build-dev.sh`。
