# 补证 E：**非空集**下的 system prompt 每轮重设（test_case / work item `wi-proof-e`，milestone `m-fix`）

> 日期：2026-10-05 · 角色：test_case
> 交付物：`seelebridge/runtime_role_prompt_matrix_test.go`（1 个用例 + 一张「装配数 × 预期」表）
> 边界：**只加用例，不改产品代码**（临时探针用完即恢复，见 §6 的 hash 校验）
> 依赖版本：`git rev-parse HEAD` = `3daee40cbc76773a0467085ee8bd9e935f76e80f`；
> 本用例依赖的产品文件 `seelebridge/runtime_role_plugins.go` = `git hash-object` `6f3df7a9a1c02042ac069f8cfe7ee5efd8847a18`、
> `seelebridge/runtime_role_turn.go` = `4828219dc976fbe29d4dad1c3349b0f76362c0c8`（我的工作区、探针前后的同一个值）

---

## 1. 缺口（复核实读的那一条）

`runRoleRound` 在进入回合闸门后**每轮重设** system prompt：

```go
engine.SetSystemPrompt(spec.SystemPrompt)   // spec.SystemPrompt 已在函数开头 append 过技能目录段
```

这是行为改动（impl §4.4 自认）。此前**只钉了空集那一侧**（`roleSkillCatalog(nil)==""`
⇒ 目录段为空串 ⇒ 字节逐字不变），**非空集那一侧没人钉过** ⇒ 复核无法构造反例：
"每轮重设"到底还是不是幂等替换、目录段会不会丢、会不会每轮多一份，全是读码结论。

## 2. 钉的**不变式**（一句话）

> 同一个角色会话跑 3 轮：**非空装配**的每一轮 system 字节 `== base + "\n\n" + catalog`
> 且目录段**恰好出现一次**、base 部分逐字未被改写；**空装配**的每一轮 system 字节 `== base`
> 且目录段出现 **0** 次。

即：`SetSystemPrompt` 的这个新调用点被钉成**幂等替换**——既不是"只第一轮带目录"，
也不是"每轮再加一遍"。`base` 取生产路径的输入 `roleTurnSystemPrompt(role)`
（见 `runtime_teamwork.go` 的 `workerRoleRoundSpec`），"字节相等"才有生产含义。

## 3. 装配数 × 预期（这张表就是断言输入，空集与非空集走**同一个断言函数**）

基座：宿主全局激活 `cad`（`cad_*`）；工具面写死 `cad_draw / doc_read / doc_edit / write_file / get_time`，
其中 `write_file` 不属于任何插件；三个插件各带一条技能（目录段逐行不同）。

| # | 装配数 | `spec.Plugins` | 本轮 ctx 集合 | 本轮可见工具面 | 目录段 | system prompt 期望 | 上限内 |
|---|---|---|---|---|---|---|---|
| 1 | 0（**空集 = 继承宿主**） | `nil` | `nil`（不是空切片） | `[cad_draw]`（宿主全局） | **不注入**（0 次） | `base`（逐字节） | — |
| 2 | 1（docs） | `["docs"]` | `["docs"]` | `[doc_read, doc_edit]` | 注入 1 次，含 `docs-guide` | `base + "\n\n" + catalog(docs)` | 1 ≤ 3 |
| 3 | 2（docs+ops，include 并集） | `["docs","ops"]` | `["docs","ops"]` | `[doc_read, doc_edit, get_time]` | 注入 1 次，含 `docs-guide`,`ops-runbook` | `base + "\n\n" + catalog(docs,ops)` | 2 ≤ 3 |
| 4 | 3（cad+docs+ops，**恰好到上限**） | `["cad","docs","ops"]` | `["cad","docs","ops"]` | `[cad_draw, doc_read, doc_edit, get_time]` | 注入 1 次，含三条技能 | `base + "\n\n" + catalog(cad,docs,ops)` | 3 ≤ 3 |

**"不对称"因此是被声明的行为**：`promptMatrixCase.wantCatalog` 显式写出"这一行该不该有目录段"
（空集 false、其余 true），不是"没测到的地方"。表本身还带三条自我校验：① 每行 `len(plugins) ≤ maxPluginsPerTeammate()`（超限走编排入口的显式拒绝，不在本表范围）；② 非空行的 `catalog` 非空、空集行必须为空；③ 四个目录段两两**逐字不同**（否则后三行退化成同一份期望，没有判别力）。

同一个断言函数 `assertPromptMatrixCase` 对四行各做 7 条检查：

1. ctx 集合：空集必须 `nil`（"没装配"与"装配了零个"同一形态）；
2. 工具面逐元素相等 + 遍历断言 `write_file` 永不可见（装配只做减法）；
3. 期望字节**独立复述**追加纪律（`base + "\n\n" + catalog`，**不调用** `appendSkillCatalog` 当期望，免得断言跟着实现一起变）；
4. 引擎上**每一份**被设置过的 prompt（建会话那次 + 每轮那次）都 `==` 期望，且观测次数 `≥ 轮数`；
5. 每份 prompt 里目录段标题 `## Available Skills` **出现次数 == wantCatalog?1:0**，目录尾句 `cannot switch plugins` 同判（重复检测串两条）；
6. 非空：`strings.HasPrefix(prompt, base)` 且余下**正好**是 `"\n\n"+catalog`（未改写提示词本体）+ 期望技能名都在 + 技能正文标记 `PROMPT-BODY` 不在目录段；空集：`prompt == base`（一个换行都不许多）；
7. **判别力对照**：把目录段再追加一遍（累积型退化的形态）⇒ 检测串必须数出 2 次且与预期不等。没有这条，第 5 条的 `want 1` 可能是"检测串根本不会命中"式的空断言。

## 4. 它排掉了什么错（一句话）

**它排掉"每轮重设 system prompt"被写成非幂等替换的两种退化：累积型（每轮 +1 份目录段，
每轮常驻 system 字节随轮数膨胀、模型看到重复目录）与丢失型（只第一轮带目录、第二轮起员工
看不见自己装配出来的技能），外加"追加时改写了提示词本体"与"空集被当成装配了空集"；
顺带把"非空集"从读码结论变成可复核的字节事实。**

## 5. 证据（命令与输出）

```text
$ gofmt -l seelebridge/runtime_role_prompt_matrix_test.go     # 无输出
$ go vet ./seelebridge/                                       # 无输出

$ go test ./seelebridge/ -run 'TestRoleRoundPromptIsResetIdempotentlyAcrossRounds' -count=3 -v
=== RUN   TestRoleRoundPromptIsResetIdempotentlyAcrossRounds
--- PASS: TestRoleRoundPromptIsResetIdempotentlyAcrossRounds (0.02s)
=== RUN   TestRoleRoundPromptIsResetIdempotentlyAcrossRounds
--- PASS: TestRoleRoundPromptIsResetIdempotentlyAcrossRounds (0.00s)
=== RUN   TestRoleRoundPromptIsResetIdempotentlyAcrossRounds
--- PASS: TestRoleRoundPromptIsResetIdempotentlyAcrossRounds (0.00s)
PASS
ok  github.com/RedHuang-0622/seelex/seelebridge  0.222s

$ go test ./seelebridge/ -count=1                             # 整包回归（含既有 AssemblyChain 六条）
ok  github.com/RedHuang-0622/seelex/seelebridge  15.522s
```

## 6. 判别力（两个**临时探针**：把产品代码改坏 → 用例必红 → 恢复）

探针**不是**交付物，只为证明"上面那串绿色不是空断言"。恢复用 `git hash-object` 校验（前后的值见页首）。

**探针 ①（累积型退化）**：把回合内的 `engine.SetSystemPrompt(spec.SystemPrompt)` 改成
`engine.SetSystemPrompt(appendSkillCatalog(spec.SystemPrompt, r.roleSkillCatalog(assembly)))`
（= 建会话与每轮各追加一遍），`-count=1` 立刻红：

```text
--- FAIL: TestRoleRoundPromptIsResetIdempotentlyAcrossRounds (0.02s)
    runtime_role_prompt_matrix_test.go:173: 1 个插件（docs）：第 1 次设置的系统提示与预期不等（每轮重设必须是**幂等替换**）：
         got  "...## Available Skills\n- docs-guide: how to read the docs\n…\nThese skills come from this teammate's assembly…
              ## Available Skills\n- docs-guide: how to read the docs\n…\nThese skills come from this teammate's assembly…"
        want "...## Available Skills\n- docs-guide: how to read the docs\n…\nThese skills come from this teammate's assembly…"
```

**探针 ②（丢失"每轮重设"这个动作）**：删掉回合内那一行 `SetSystemPrompt`，同一用例红：

```text
--- FAIL: TestRoleRoundPromptIsResetIdempotentlyAcrossRounds (0.01s)
    runtime_role_prompt_matrix_test.go:173: 空集（继承宿主、目录不注入）：只观测到 1 次 system prompt 设置（少于轮数 3）
```

（② 的诚实口径：这一条判的是"每轮重设**被观测到**"这个行为本身还在不在——它不代表引擎里的
字节当时是错的，只代表"每轮重设"这个 impl §4.4 的自认改动没有被静默删掉。）

## 7. 残留与依赖

- **依赖 wi-fix-core 的那一版**：本用例只用 `roleTurnSystemPrompt` / `roleSkillCatalog` /
  `appendSkillCatalog` / `rolePluginAssembly` 与 `runRoleRound` 的**行为**，全部是装配集合的
  非空/空两类。若 `wi-fix-core` 改了目录段渲染或读数结构，本用例要么继续绿（渲染口径未动）、
  要么红（那就是证据，把红原文贴回来并注明依赖 `runtime_role_plugins.go@6f3df7a9`）。
- **未覆盖**：真引擎（`*session.Session`）对 `SetSystemPrompt` 是否**替换**语义——本用例用的是
  假引擎（探针），"幂等替换"在真引擎上与 Seele 模块的实现绑定，仓库内无 vendor，无法在本用例里核。
- **未覆盖**：同一会话的装配集合在轮与轮之间**变化**（计划改装配）时目录段是否随之更新——编排面
  只改 `pending` 工作项，本里程碑不涉及。
