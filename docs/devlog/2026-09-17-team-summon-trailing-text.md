# 2026-09-17 `@` 召唤团队：名字后面的那句话不再被吞掉

## 症状

在会话里输入

```text
@goal-a2a 这次启动团队主要是看看整个team的工作是否打通。
```

回执是：

```text
未知团队: goal-a2a 这次启动团队主要是看看整个team的工作是否打通。
@ 手动召唤团队：@<团队> 把一支团队装配到当前会话（入伙切点 = 当前消息尾）。
@goal-a2a 顺序 user→main→tl · 3 个角色
...
```

即：**团队名没被认出来，而且用户那句话消失了**——没有装配、没有输入、没有错误。

## 根因

`application/core/input_router/router.go` 的 `teamRoute.Dispatch` 把 `@` 之后的整段余量
原样当作团队名（只 `TrimSpace`）：

```go
func (route teamRoute) Dispatch(ctx context.Context, input string) error {
	return route.dispatch(ctx, strings.TrimSpace(strings.TrimPrefix(input, "@")))
}
```

对比 `skillRoute`（`$name args`）用的是 `strings.Fields`，把首段当名字、其余当参数。
于是 `@goal-a2a 这次启动团队…` 的"名字"= 整句，`resolveTeamSummon` 拿整句去比对
内置形态与团队库条目，全部落空 → `unknownTeamNotice`。

这个"整段当名字"不是随手写的：团队库条目允许用户起**含空格**的名字，按空格硬切会
把它们切坏（`application/core/input_team.go` 的注释与
`docs/devlog/2026-09-17-input-sigils-and-manual-team-summon.md` 都记着这条取舍）。
缺的是另一半：名字后面跟的话怎么处理。

## 变更（`application/core/input_team.go`）

切分留在应用层（路由保持"零 I/O、零团队知识"，且不改签名）：

- `resolveTeamSummon(sessionID, name) (target, tail, ok)`：候选从整串开始按空白边界
  逐级回退（`teamNameCandidates`：`a b c` → `a b c`、`a b`、`a`），**命中的最长前缀 =
  团队名，余下 = 附言**。每个候选都是原串的字节前缀，所以 `name[len(candidate):]`
  恒为附言。
- `teamSummonIndex`（`presets` + `library` 两组）把团队库**只读一次**：候选逐个查表，
  不再逐个读盘；`match` 保留旧判据（内置形态看 `team_id`/`team_kind`，库条目看
  `team_id`/`name`，内置优先，匹配不区分大小写）。
- `submitTeam`：装配成功且 `tail != ""` 时，补 `prepareCompletedTaskBoundary()` 后
  `submitConversation(ctx, SigilTeam+name)`——附言按 `$<skill> <args>` 的**同一条口径**
  作为一条输入下发，原文一并交给会话，与 `activateSkillAndSubmit` 一致。
- `teamSummonNotice(target, result, tail)` 在有附言时明说"附言已作为本会话的一条输入
  下发"，免得用户以为那句话还是被吞了。
- 失败口径：全部候选落空时用 `presumedTeamName`（首个 token）报"未知团队"，不再把
  用户整句回显成名字；未装配成功就不下发任何输入。
- `teamSummonHelp()` 自述改为 `@<团队> [附言] …`。

文档与注释同步：`application/core/completion.go`（sigil 表）、
`application/core/input_router/router.go`（teamRoute 与 skillRoute/pluginRoute 的差别）、
`docs/gui/decisions.md` ADR-GUI-021、`docs/gui/modules/shell-and-interactions.md` 第 6 节、
`docs/gui/CHANGELOG.md`。

## 口径决定（写下来免得下轮 review 重新推）

1. **不按空格硬切**：团队名可以含空格（库条目用户起名），硬切会把
   `@审计 小队` 切成 `审计` + 附言 `小队`。最长可命中前缀兼顾两头。
2. **最长优先**：库里同时有 `审计` 与 `审计 小队` 时，`@审计 小队 请审核` 命中后者，
   而不是前者 + 附言 `小队 请审核`。
3. **附言下发而不是丢弃**：丢弃等于把用户的话吃掉（也就是本次的症状）；
   `$<skill> <args>` 已经确立了"带参数就作为一条输入下发"的先例。
4. **未命中只报首个 token**：候选已经逐级试过，全部落空时把半句话当名字只会让人
   对着自己那句话发愣；提示里也带了用法一行。
5. **前端与 TUI 不改**：它们提交的是原文，`Suggestions` 在进入参数区（含空格）时
   本就不弹建议，与新写法一致。

## 验证

单元/契约（`application/core`、`input_router`）：

```text
go build ./...                                                          → ok
gofmt -l application                                                    → 空
go test ./application/core/ ./application/core/input_router/ \
  -run "SubmitTeam|Sigil|Suggestions" -count=1 -v
→ TestSubmitTeamSummonsPresetTeam / TestSubmitTeamWithoutNameDescribesPresets /
  TestSubmitTeamSummonsLibraryEntryByName / TestSubmitTeamTrailingTextIsSentAsInput /
  TestSubmitTeamResolvesMultiWordLibraryNameWithTrailingText /
  TestSubmitTeamUnknownNameWithTrailingTextReportsNameOnly /
  TestSubmitTeamUnknownNameGivesActionableNotice /
  TestSigilMigrationHintsPointAtTheRightPrefix / TestSuggestionsAndSkillRouting /
  TestInputRouterSigilOwnership 全 PASS
```

新增用例钉住的是三件事：① 附言触发装配 + 附言作为一条输入下发（含入伙切点仍是
装配那一刻的消息尾 seq）；② 含空格的名字 + 附言切对（最长前缀赢）；③ 未命中时
只报首个 token、不装配、不下发。

真实装配端到端（真 application + 真 sessionstore）。注意：**会话树不在 `-store`
目录下，而在 `filepath.Dir(storePath)` 的 `sessions-json/` 里**（`initStore()` 用
`filepath.Dir(*storePath)` 当 baseDir）。

```text
# ① 只装配
go run . -frontend backend -backend-prompt "@goal-a2a" \
  -store tmp/e2e-teamwork/store-<stamp> -backend-log tmp/e2e-teamwork/log-<stamp>.log
→ exit=0；stage=submit input_chars=9 + stage=chat.idle
  tmp/e2e-teamwork/sessions-json/project-e3b0c44298fc1c14/session-c26b81d3c49d173b/
    team/roles.json         → team_id=goal-a2a, order_policy=goal_loop,
                              roles=[main(builtin), user(builtin), tl(techlead, readonly)]
    metadata/lifecycle.json → payload.order_roles=["user","main","tl"]
    goal_a384d51d11c6689e/…/session-a384d51d11c6689e/  → tl 角色会话（role_session_id=goal-a2a-tl, join_seq_id=0）
    metadata/message.json   → 不存在（只装配，没有任何输入）

# ② 装配 + 附言
go run . -frontend backend \
  -backend-prompt "@goal-a2a 这次启动团队主要是看看整个team的工作是否打通。" \
  -store tmp/e2e-teamwork/store-fix-<stamp> -backend-log tmp/e2e-teamwork/log-fix-<stamp>.log
→ stage=submit input_chars=35 + stage=chat.started（附言真的作为一轮对话起了）
  …/session-e61c6b851bd0a173/
    team/roles.json / metadata/lifecycle.json  → 与 ① 逐字段一致
    metadata/message.json  → payload.meta.summary =
        "@goal-a2a 这次启动团队主要是看看整个team的工作是否打通。"
```

②的那次 chat 因为后端控制台用默认 `-permission manual`，模型起手要审批
（`toolhook.start.enter tool=skill_activate` → `interaction.opened`）而卡到
`-backend-timeout`，最终 `chat.idle.error error="context deadline exceeded"`（exit=1）。
这是**探针参数**造成的（没人点审批），不是本次改动的问题：装配与"附言作为输入"两步
都已落盘可证。

## 未做

- 前端不做"`@` 之后再给附言"的专门 UI（composer 仍是自由文本，提交原文）；
  如果以后要在 UI 上把"名字"与"附言"分开渲染，那是显示层的事，不影响本口径。
- 附言里的 `@<团队>` 前缀不裁剪（原文交给会话，与 `$` 一致）。
