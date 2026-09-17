# 2026-09-17 输入前缀一字符一含义 + `@` 手动召唤团队

## 范围

两件事，共用一条链路：

1. **前缀（sigil）改口径**：旧口径是 `/` 命令、`#` Skill、`@` Plugin。这三个符号
   没有可推断的语义（`#` 像标签、`@` 像 mention，却分别指向 Skill 与 Plugin）。
   新口径与用例一一对应：`/` 命令与工具、`#` 切换 Plugin、`$` 召回 Skill、
   `@` 手动召唤团队。
2. **补 `@` 的真链路**：团队此前**只能**由 goal 上线时自动装配
   （`application/core/goal_service.go` 的 `ensureGoalAgentTeam`，preset `goal-a2a`）；
   `docs/devlog/2026-09-13-visible-role-attribution.md` 当时就记了"召唤出 teamwork
   仍只有 goal-a2a → ADVISOR 一条真链路"。本次给人补上对称的显式入口。

## 变更

- `application/core/completion.go`：sigil 常量与唯一枚举（`SigilCommand/Plugin/
  Skill/Team`）、`splitSigil`、`HasSigilPrefix`/`SigilOf`（多前端共用判定）、
  `sigilMigrationHint`（旧前缀未命中时指路），以及按域拆开的建议构造器；
  `@` 只列内置团队形态（**零 I/O**：`Suggestions` 在 TUI 的 `View()` 渲染路径上，
  不能在渲染循环里读团队库）。
- `application/core/input_router/router.go`：路由改为 command → plugin → skill →
  team → conversation，`RouteHandlers` 新增 `Team`；`@` 空名也下发（召唤面自述）。
- `application/core/input_team.go`（新增）：`@` 的落点。解析顺序 = 内置形态（零 I/O）
  → 团队库条目（一次读，team_id 或用户起的名字都能命中）；装配复用既有工厂与
  `AgentTeamMaterializeTeam`/`MaterializeAgentTeamPreset`，入伙切点取
  `teamJoinSeqFor`（= 装配那一刻主会话消息尾 seq，与自动路径同一条判据），不新增
  第二份团队事实。
- `application/core/input.go`+`service_assembler.go`：`#` 落到 `submitPluginSwitch`
  （失败且名字其实属于别的域时补迁移提示），`$` 落到 `submitSkill`，装配 `Team`。
- `tui/`：`suggMode`/前缀渲染/接受建议都改为 `application.HasSigilPrefix`、
  `application.SigilOf`，TUI 不再自持字符表。
- 前端：`app.js` 的 `SIGIL_*`/`SIGILS`/`SIGIL_PATTERN`（含"提交 `@` 后强制重取
  Agent Team 面板"）、`index.html` 的触发器按钮与空态提示、`chat-view.js` 的
  composer 占位、`components.js` 新增 `team` 图标。
- 文档：`docs/gui/decisions.md` ADR-GUI-021、`docs/gui/modules/shell-and-interactions.md`
  第 6 节、`docs/gui/modules/application-protocol.md`、`docs/gui/CHANGELOG.md`、
  `docs/arch/skill-effort-architecture.md`（`#review/#end` → `$review/$end`）、
  `tui/README.md`、`gui/frontend/README.md`，`application/core` 分卷 README 经
  `scripts/gen_core_readme_index.py` 刷新。

## 验证

单元/契约用例（新增或改口径）：

```text
go test ./application/core/ ./application/core/input_router/ -run "Sigil|SubmitTeam|Suggestions" -count=1 -v
→ TestInputRouterSigilOwnership / TestSubmitTeamSummonsPresetTeam /
  TestSubmitTeamWithoutNameDescribesPresets / TestSubmitTeamSummonsLibraryEntryByName /
  TestSubmitTeamUnknownNameGivesActionableNotice / TestSigilMigrationHintsPointAtTheRightPrefix /
  TestSuggestionsAndSkillRouting 全 PASS

go test ./tui/... -count=1              → ok（含 TestSigilPrefixesDriveSuggestionMode）
node --test gui/frontend/dist/*.test.mjs → tests 322 / pass 322 / fail 0
gofmt -l application tui gui            → 空
go build ./... ✓ / go build -tags "gui,desktop,production" ./... ✓
go vet ./application/... ./tui/...      → 空
go test ./application/... ./tui/... ./gui/... -count=1 → 全 ok
```

真实装配端到端（真 application + 真 sessionstore，一次性 store 目录，跑完即删）：

```text
go run . -frontend backend -backend-prompt "@goal-a2a" -store tmp/sigil-probe/store-<stamp> -backend-log <log>
→ exit=0，stage=startup.application.ready + stage=submit input_chars=9 + stage=chat.idle

落盘证据（store 内的会话树）：
  session-<id>/team/roles.json      → team_id=goal-a2a, order_policy=goal_loop,
                                      roles=[user(builtin), main(builtin), tl(techlead, readonly)]
  session-<id>/metadata/lifecycle.json → payload.order_roles=["user","main","tl"]
  goal_<id>/.../session-<id>/        → tl 的角色会话目录（含 event/metadata）已建
```

同口径的负向证据：`go run . ... -backend-prompt "@"`（空名）→ exit=0，store 内
**没有**任何 session/team 文件落盘（只有 store 目录的 `.lock`）。notice 正文本身
不由 backend frontend 回显，因此空名自述的文案由单元用例
`TestSubmitTeamWithoutNameDescribesPresets` 覆盖。
