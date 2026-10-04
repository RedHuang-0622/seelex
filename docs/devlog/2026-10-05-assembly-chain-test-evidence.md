# 装配链路的用例与回归证（test_case / work item `wi-test`，milestone `m-build`）

> 日期：2026-10-05 · 角色：test_case
> 交付物：`seelebridge/assembly_chain_test.go`（6 个用例，覆盖 leader 点名的五组）
> 前置：`wi-assemble` 的装配链路（`seelebridge/runtime_role_plugins.go` + `seelebridge/tools/policy.go`
> 的 `PluginFace` 收口 + `plugin/curated.go` 的 `Assemble*` 闸 + `plugin/manager.go` 的
> `ActivateFromCatalog`）
> 定位：**证据与回归证**，不改产品代码。

---

## 1. 链路（被测对象）

```
team_plan members[].plugins → plan(sessionstore.TeamworkMember.Plugins)
  → WorkerRequest.Plugins → roleRoundSpec.Plugins
  → rolePluginAssembly（显式声明 > 角色自带 > 空集）
  → 本轮 ctx（seeltools.WithRolePlugins）
  → tools.Policy.Filter 的最后一道 PluginFace（工具可见面）
  → 同一集合另渲染成技能目录段追加进 system prompt + 装配读数进 team_plan 回执
精选目录侧：plugins/curated.yaml → plugin.CuratedCatalog.AssemblePreset / AssemblePlugin
  → plugin.Manager.ActivateFromCatalog（pending 在任何副作用之前被拒）
```

判定的**顺序即语义**：权限面（`ToolFace`）先算、插件面（`PluginFace`）后算 ⇒ 插件只做减法。
装配集合刻意**不缓存在任何句柄上**（每轮从 ctx 现算）。

## 2. 用例清单（每条都写了"排掉了什么错"）

| 用例 | 钉什么 | 排掉的错 |
|---|---|---|
| `TestAssemblyChainTeammateSeesOwnPluginsToolsAndSkills` | 装 cad 的只见 `cad_draw` + `cad-tips`；装 docs 的只见 `doc_*` + `docs-guide`；装 cad+docs 的是 **include 并集**；全局激活态未被改 | 装配只进 ctx 不落到工具面/目录；两个 teammate 撞面；装配与宿主全局激活态混合；目录读全局 `activePlugin` 被掩蔽 |
| `TestAssemblyChainConcurrentTeammatesDoNotCrossTalk` | 两个 teammate 的回合在**会合点**对齐着同时在飞（各 3 轮，`arrived`/`release` 通道，**不用 sleep**），各自可见面与目录稳定；root 面与全局激活态不变 | 把"当前装配"缓存在句柄/Policy/全局上（后写覆盖先跑，即并发丢失更新的另一面） |
| `TestAssemblyChainEmptySetIsByteIdenticalToToday` | ① 空集工具面与**旧收口**（`PolicyDeps{PluginFilter: r.plugins.Filter}`）逐元素相等，且旧收口非空；② 引擎上被设置的**每一份** prompt 与 `roleTurnSystemPrompt(role)` **逐字节相等**；③ `roleSkillCatalog(nil/[]string{}) == ""` 且 `appendSkillCatalog(p,"")==p`；④ 阴性对照：同一注册表下非空集合确实渲染出目录段 | 把"没声明 plugins"误实现成"装配了空集"（工具面变空 / system 字节被改 / 注入全宿主技能） |
| `TestAssemblyChainRejectionsSpellOutTheReason` | 未知名（"未定义/显式拒绝/不静默忽略/ghost"）、超上限（"上限 3/声明了 4 个/不静默截断"）、只留空白项=空集；**且每种被拒之后落盘的仍是上一份合法计划** | 未知名被静默忽略；超限被静默截断；拒绝发生在写盘之后（报错但计划已落地） |
| `TestAssemblyChainPendingPluginCannotBeAssembled` | 带证据档的 pending 是合法路线图；`AssemblePlugin`/`AssemblePreset` 对 pending **点名 pending + 来源**拒绝；**链的下一跳** `team_plan` 对同一名字也拒（"未定义/显式拒绝"）且不落盘，而目录放行的那个被 team_plan 收下 | 把路线图当可用集合（静默跳过 → 看起来正常、实际空的能力面）；两处闸门对同一名字给出不同答案 |
| `TestAssemblyChainReceiptCarriesCatalogReadingsAndYellow` | 回执带 `plugin_limit_per_teammate` + 逐成员 `assemblies`（mode/plugins/skill_count/`skill_catalog_runes`/`skill_catalog_tokens_est`/`plugin_face_tools`/`total_tools`/yellow）；空集写 `inherit-host`；`[" ops ","ops"]` 规整写回；黄牌判据（>6k ⇒ 黄且**照旧受理**；窗口 2% 是更紧那道门）；`config/seelex.yaml` 的 `limits.plugins.per_teammate` 读得出 3 | 成本不可见（装了什么都不报字节）；空集靠字段缺失暗示；黄牌判据写反（该报不报/该放行却拒） |

## 3. 证据（命令与输出）

```text
$ gofmt -l seelebridge/assembly_chain_test.go        # 无输出
$ go vet ./seelebridge/                              # 无输出

$ go test ./seelebridge/ -run 'AssemblyChain' -count=1 -v
--- PASS: TestAssemblyChainTeammateSeesOwnPluginsToolsAndSkills (0.02s)
--- PASS: TestAssemblyChainConcurrentTeammatesDoNotCrossTalk (0.00s)
--- PASS: TestAssemblyChainEmptySetIsByteIdenticalToToday (0.00s)
--- PASS: TestAssemblyChainPendingPluginCannotBeAssembled (0.01s)
--- PASS: TestAssemblyChainRejectionsSpellOutTheReason (0.00s)
--- PASS: TestAssemblyChainReceiptCarriesCatalogReadingsAndYellow (0.01s)
ok  github.com/RedHuang-0622/seelex/seelebridge  0.252s

$ go test ./seelebridge/ -run 'AssemblyChain' -race -count=3 -v
（6 用例 × 3 轮 = 18 次全 PASS；无 WARNING / 无 DATA RACE）
ok  github.com/RedHuang-0622/seelex/seelebridge  1.417s

$ go test ./seelebridge/... ./plugin/ ./skill/ ./sessionstore/ ./seelexctx/ \
         ./application/core/agentteam/ . ./e2e/ -count=3
ok  .../seelebridge 80.493s        ok  .../seelebridge/tools 54.442s
ok  .../seelebridge/teamwork 2.187s  ok  .../sessionstore 152.290s
ok  .../plugin 3.113s              ok  .../skill 2.431s
ok  .../application/core/agentteam 1.366s   ok  .../seelex 48.234s   ok  .../e2e 0.808s
FAIL .../seelexctx —— TestPrefixChainLockContentionGate（见 §4 发现 1，与本链路无关）

$ go test ./application/core/agentteam/ . ./e2e/ -count=3     # 去掉 seelexctx 后全绿
ok .../application/core/agentteam 1.366s   ok .../seelex 48.234s   ok .../e2e 0.808s
```

被依赖文件的内容哈希（`git hash-object`，证明用例跑在哪一版上）：

```text
plugin/curated.go                  e3f44b1874f06ef4bd409a56c14aea27e85a3c11
plugin/manager.go                  37a5d1584c53912952607b78f8bd7976fa791616
plugin/loader.go                   c784f73b1ccdc82ac3414e7be685ac8309002145
main.go                            d5ae4bf80909a46a683f9ad4cd340d8a1a1c60de
seelebridge/runtime_role_plugins.go 6f3df7a9a1c02042ac069f8cfe7ee5efd8847a18
seelebridge/runtime.go             b9a4507a6d87131e1e625a63e6771073b6ca9fd6
seelebridge/tools/policy.go        ce55611f248187b1109768a91075d30c6a4ee23a
seelebridge/assembly_chain_test.go 2fcacb414c794c46579ac6ad6c24c86fd8600189
skill/skill.go                     a478c321bc9d750a59da7d2489bbe7b8bc9b3f46
seelexctx/limits.go                810c58b3a57d5d8639e00615ee5a46c15e8d8406
e2e/layout_test.go                 39645f882fb95f38d9ae22dc9e5a8ac1fc87ff0c
```

## 4. 独立发现（都不是本文件引入的，按处置优先级）

1. **【中】`seelexctx.TestPrefixChainLockContentionGate` 与 `-count≥2` 不兼容**（挡住"全仓 `-count=3`"这条证据口径）。
   证据：`go test ./seelexctx/ -run TestPrefixChainLockContentionGate -count=1` → `ok`；
   同一条命令 `-count=2` → `FAIL`（A 臂断言，2 个样本，count 逐轮完全相同 ⇒ 确定性而非抖动）。
   根因（读码 + 帧）：`runtime.SetMutexProfileFraction(1)` 打开的是**进程级累积** profile，A 臂
   `dumpProfile(t,"mutex-pure")` 把**上一轮** B 臂（`contentionGateSharedWorker` / 测试内 `func3`）留下的
   `memoryCompactStack.Snapshot` / `PushCompact` 样本一起读了出来，而 A 臂的容忍度是零。
   处置二选一（本工作项不改别人的面）：A 臂前后 `SetMutexProfileFraction(0)` 再置回 1 重置累积；
   或按"样本是否落在本臂符号上"过滤而不是全量断言。
2. **【中，流程】工作区在我这轮里被并发改动，公开 API 在半途变更**：
   01:50 我读到的 `plugin.CuratedPreset.Pending` 是 `[]string`，01:56 首次编译已变成
   `[]CuratedPending`（并新增 `AssemblePreset`/`AssemblePlugin`/`ActivateFromCatalog`），
   编译报错 `cannot use []string{…} as []plugin.CuratedPending`。同一工作区无 worktree 隔离时，
   "先写用例、后跑证据"会把证据钉在移动靶上（哈希见 §3，仅代表这一刻）。
3. **【低，文档漂移】设计稿与实现差一代**：`2026-10-05-teammate-plugin-assembly-design.md` §5 明写
   "精选目录 / marketplace 的收集：本轮不做"，但精选目录（`plugins/curated.yaml` + `plugin/curated.go`
   的 pending 证据档）与两条装配闸已落地并被 `e2e/layout_test.go` 守卫；两份 devlog（design/impl）
   里 `Assemble` 零命中（只有 `plugins/README.md:40` 记录）。
4. **【低，读数口径】回执里 `inherit-host` 成员的 `plugin_face_tools`/`total_tools` 是 0/0**：
   空集成员继承的是**宿主当前面**（非零），报 0 容易被读成"零能力"。建议空集成员也报
   "继承宿主当前面"的读数。另：`dto.NormalizePlugins` 对重复**静默去重**（用例已断言写回 `["ops"]`），
   而 `sessionstore.ValidateTeamworkPlan` 对重复是**显式拒绝**——经 `team_plan` 这条路后者不可达，
   同一事实两处口径。

## 5. 未验证 / 边界（诚实标注）

- 会合点覆盖的是**跨会话**并发（同会话并发回合被 `roundGate` 串行，这是设计），
  "同一句柄上被后写覆盖"这一形态不在本用例范围内。
- 没有真进程 + 真模型 + 两个 teammate 的端到端复现（需要 GUI/账号，属验收面）；
  本文件全部用注入的 `roleEngine` 探针 + 运行期 `Policy` 实例走**生产那条收口**。
- 用例依赖 `plugin.AssemblePreset/AssemblePlugin` 与 `plugin.CuratedPending` 的当前形状；
  若该 API 改名，本文件需同步（发现 2 的代价）。
