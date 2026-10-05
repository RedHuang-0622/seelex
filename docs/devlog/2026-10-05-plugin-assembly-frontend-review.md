# 前端向审查：插件装配的显示（2026-10-05）

> 本轮由 leader 派活，board-ui / editor-ui / bridge-eng 三席并行实现，verifier（独立验证）与
> front-reviewer（前端向审查）两席只读收口。本文件是审查交付物 + 验证证据，由 leader 落盘
> （两位审查席的只读工具面无写入口，正文原样取自其回执，见 session 的 teamwork 作业记录）。

## 一、审查对象（本轮提交）

| 提交 | 内容 | 件 |
|---|---|---|
| `39a8b63` | 装配读数进团队看板投影（dto + seelebridge，leader 手工集成） | wi-projection |
| `a68bf68` | 团队看板成员行显示插件装配（chips + 技能/目录/token/工具面 + 黄牌/失灵/已撤） | wi-board |
| `bd6e3cc` | 员工档案与编辑面板回读回传 `plugins`（修「修改员工」静默清空） | wi-editor |
| `8294dcb` | 员工提交侧接线（`agentTeamRolePayload` / hire submit）+ `plugin-source.js` 纯函数 | wi-editor-submit |
| `118f259` | 插件来源读数（`source_kind`/`source_url`/`source_root`）进运行时视图 | wi-prov-be |
| `2cb14c3` | 「运行状态 → Plugins」每行标来源 + 面板上方那句「当前进程加载到的那一份」 | wi-prov-ui |
| `39e65ac` | 审查 P0 收口：看板装配行对齐 nil 契约 | wi-fix-board |
| `283dfda` | 审查 P0/P1/P2 收口：未登记仍报载入根、上限不编数字、转义/切分等价用例 | wi-fix-plugins |

## 二、验证证据

leader 亲跑（本机，主工作区）：

| 命令 | 结果 |
|---|---|
| `node --test team-board-view.test.mjs agent-team-view.test.mjs plugin-source.test.mjs` | **95 tests / 95 pass / 0 fail**（末次，`283dfda` 之后） |
| `node --check` 四个改动 js | exit 0，无输出 |
| `go build ./...` | exit 0 |

verifier（独立只读核，`a10`）：

| 命令 | 结果 |
|---|---|
| `go test ./gui/ ./application/... ./seelebridge/ ./plugin/ ./internal/adapters/ -count=1` | 全 `ok`，无 FAIL |
| `go test . -run TestTeamworkPluginAssemblyHeadlessSmoke -count=1 -v` | PASS；读数：`ui-eng=replace/[impeccable] 技能=1 目录=585 字节 面=57/57 · plain-eng=inherit-host 面=57/57` |
| `go build -tags "gui,desktop,production" ./...` | exit 0 |
| `go vet -tags "gui,desktop,production" ./application/... ./plugin/ ./internal/adapters/ ./seelebridge/ ./gui/` | exit 0 |
| `git diff --check HEAD~8 HEAD` | 空 |

契约逐条（verifier 结论，全部 ✓）：`member.plugins` / `member.assembly` 的键名与 omitempty；
空集 = 显式 `inherit-host`（非缺字段）；`plugin_face_faulted/missing` 不被读成「没装配」；
`source_kind/source_url/source_root` 未登记时整键缺席、不编默认值；`source_root` = 多根
first-wins 后的载入根。

**真机目视 = 未验证。** 当前 GUI 进程 15:19 启动，而本轮提交最早 15:48 落 —— 新构建的
`members[].assembly` 与 `runtime.plugins[].source_kind` 不可能在旧进程里渲染。要看到屏幕上的
效果，需**重启 GUI 到 HEAD 构建**。

## 三、审查结论（逐条）

1. **看板成员行显示装配**（`a68bf68` → `39e65ac`）：语义骨架正确（Go 投影用例绿），但初版
   `team-board-view.js:504` 把「`assembly` 缺失」渲染成「继承宿主」+「按不覆盖处理」，
   **与冻结契约 `application/contract/dto/teamwork_board.go:121-123` 相左**（该注释明写 nil ⟹
   前端不显示装配格），且「声明非空 + 读数缺失」会同时渲染 chips 与「继承宿主」（两句互斥）。
   → **已修**（`39e65ac`）：缺失/未写明 mode 一律不下 mode 结论；声明面为空整格退场、非空只列
   声明 chips；新增两条用例钉住，原「缺失 → 继承宿主」用例按新口径改写。
2. **员工档案与编辑面板回读回传**（`bd6e3cc`）：方向正确，钉住了真实的**静默清空**回归
   （字段表/别名表/回读归一三处 `plugins` 与 `permission_groups` 同位置）。三处小漂移不阻断，
   其中「上限常量不接线」已在 3 中处置。
3. **提交侧接线**（`8294dcb`）：与后端显式拒绝口径一致（`dto.NormalizePlugins`：「重复显式拒绝、
   不静默合并」），空 = 不写键已钉；覆盖入库 `SaveEmployee` 与入职 `InstantiateRole` 两条路。
   争议点（前端曾保序去重）由 leader 裁决改为**前端不去重、重复原样提交由后端拒**，并用探针实跑
   两条入口取证。可维护性瑕疵：接线用例用源码正则匹配 `app.js` 文本，行为等价的重构即红（记为债务）。
4. **Plugins 面板标来源**（`2cb14c3` → `283dfda`）：三键读法与映射正确、未重算后端判据
   （未知 kind 原样显形）；初版**未登记插件整块退场**，把后端刻意下发的 `source_root`
   「载入根」事实一并丢掉 —— 而用户原话（「这些前端显示出来的 plugin 我没有在我的 plugin 下面
   见过」）指向的**恰恰是那批插件**。→ **已修**（`283dfda`）：无 kind 时仍报「载入根 …」，
   标签写「来源未登记」，绝不编成内置。
5. **用户困惑的解答度**：有来源的那几行解了（发行态下 default/hardware → 随发行包、
   impeccable → 第三方移植、artist/backend/frontend/teacher → 本机自建）；无来源的那几行
   现在靠「载入根」解释了。**但真机目视未做，不能下「屏幕上已解决」的结论。**

## 四、P0 / P1 / P2 清单与处置

| 级 | 问题 | 处置 |
|---|---|---|
| P0 | 看板把 `assembly` 缺失读成「继承宿主」，与 `dto:122` 契约相左、且与声明面自相矛盾 | ✅ `39e65ac` |
| P0 | 未登记插件丢掉 `source_root` 事实，最需要解释的行反而裸奔 | ✅ `283dfda` |
| P1 | `hirePanel` 未接真实上限 ⇒ 面板恒显「上限 3 个」，配置抬高后提示失真 | ✅ `283dfda`（查证上限未在快照/看板面 ⇒ 改为只报配置键 `limits.plugins.per_teammate`，不编写死数字；注入口保留） |
| P2 | `escapePluginSourceText` 与 `components.escapeHtml` 逐字重复 | ✅ 等价用例（生产码不 import components，用例里逐字比较） |
| P2 | `submitPluginNames` 与 `normalizePluginNames` 两套切分 | ✅ 不变式用例（同语料断言「只差去重」） |
| 债务 | 接线用例用源码正则钉实现细节 | 保留（记为债务，未修） |

## 五、反方立场（哪些可能是错的 / 只是装饰 / 膨胀）

- **可能是错的**：初版 nil 语义越界（已修）；`replace` 但读数缺字段时会显示「技能 0 / 工具面 0/0」，
  读者会当成真 0（未修，缺字段应属不可达）；上限提示在配置抬高后曾会骗人（已修）。
- **可能只是装饰**：来源标签的价值全部押在运行期真的下发 `source_kind`；**未登记那批**仍然只有
  「载入根」，标签集是**残缺**的 —— 这是「装饰性显示」的真实边界。
- **膨胀**：Plugins 每行 +1 行；团队看板每个成员行 +2~4 行，且「teammate 级 · 对这位每个工作项
  会话都生效」这句**逐行逐字重复**。
- **真机目视未做 ⟹ 不能下的结论**：不能说用户困惑「在屏幕上」已解决；不能说 chip/hint 在真实面板
  宽度下可读；不能断定运行时加载的正是仓库 `plugins/curated.yaml`（根链与环境相关）。

## 六、未验证清单与下一步

1. **真机目视**（需人）：重启 GUI 到 HEAD 构建 → 看「运行状态 → Plugins」的来源标签/载入根，
   看团队看板成员行的装配 chips 与读数。旧进程（15:19 起）看不到本轮改动。
2. **缺口（已登记，未做）**：① 逐根加载数（`pluginRootReport`）只在终端日志与启动通知面，
   未进 `RuntimeState`/会话快照；② `SaveEmployee`/`PutRole` 的**入口级**重复拒绝只有 scratch
   探针取证，没有固化 Go 用例（`NormalizeRole` 有用例，两条入口没有）。
3. **工具面限制**：两位只读席的 `node` 执行面被拒 ⇒ 前端套件由 leader 亲跑代偿；如需第三方独立
   复现前端数字，需给验证席开放 `node` 执行面。
