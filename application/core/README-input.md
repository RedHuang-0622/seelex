# core/input（根包分卷）

## 生态位

输入分派与路由兼容测试

覆盖：`input*.go` + 显式名单（见生成器 `ROOT_GROUPS`）；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### durable_queue_wire_test.go

- `func (s *queueRecordingSessions) QueueEnqueueWorkspace(_, _, _, content string) error`
- `func (s *queueRecordingSessions) QueueMarkConsumedWorkspace(_, _, turnID string) error`
- `func (s *queueRecordingSessions) QueueConfirmConsumedWorkspace(_, _, turnID string) error`
- `func (s *queueRecordingSessions) QueueFailConsumedWorkspace(_, _, turnID string) error`
- `func (s *queueRecordingSessions) QueueRecoverItemsWorkspace(_, _ string) (sessionstore.QueueRecoveryReport, bool, error)`
- `func (s *queueRecordingSessions) queueCalls() (enqueued, marked, confirmed, failed []string)`
- `func (*snapshotFailingSessions) SaveSessionSnapshot( string, []contract.EngineMessage, model.SessionRecord, []model.TranscriptEvent, []model.StoredToolResult, ) error`
- `func (*snapshotFailingSessions) SaveSessionSnapshotWorkspace( string, string, []contract.EngineMessage, model.SessionRecord, []model.TranscriptEvent, []model.StoredToolResult, ) error`
- `func waitQueueCalls(t *testing.T, sessions interface { queueCalls() (enqueued, marked, confirmed, failed []string) }, ready func(enqueued, marked, confirmed, failed []string) bool)`
- `func newBlockingEngine() *sessionBackedBlockingEngine`
- `func TestDurableQueueMirrorsQueuedInputAndConfirmsTurn(t *testing.T)` — TestDurableQueueMirrorsQueuedInputAndConfirmsTurn 运行中提交 → 镜像落盘；
- `func TestDurableQueueReturnsConsumedContentToDraftOnPersistFailure(t *testing.T)` — TestDurableQueueReturnsConsumedContentToDraftOnPersistFailure 快照落盘失败
- `func TestDurableQueueBackfillVisibleAndDrainedOnNextSubmit(t *testing.T)` — TestDurableQueueBackfillVisibleAndDrainedOnNextSubmit 冷加载恢复的输入

### input.go

- `func (service *Service) submitCommand(ctx context.Context, input string) error`
- `func (service *Service) submitSkill(ctx context.Context, name string, args []string, input string) error`
- `func (service *Service) submitPluginSwitch(ctx context.Context, name string) error` — submitPluginSwitch 是 `#` 前缀的落点：切换/停用插件。
- `func (service *Service) endSkill() error`
- `func (service *Service) activateSkillAndSubmit(ctx context.Context, skill SkillInfo, args []string, input string) error`
- `func (service *Service) prepareCompletedTaskBoundary()`
- `func (service *Service) applySkill(skill SkillInfo)`

### input_router_compat_test.go

- `func TestNewAssemblesInputRouter(t *testing.T)`

### input_team.go

- `func (service *Service) submitTeam(ctx context.Context, name string) error` — submitTeam 是 `@` 前缀的落点：手动召唤一支团队到当前会话。
- `func (service *Service) beginGoalForSummon(ctx context.Context, sessionID, tail string) (*goaldomain.GoalRecord, error)` — beginGoalForSummon 是"召唤即干活"的落点：`@<团队> <附言>` 里的附言是一条要干的
- `func goalTitleForSummon(tail string) string` — goalTitleForSummon 由附言派生 goal 标题：goal_begin 要求 title 必填，而召唤场景
- `func (service *Service) materializeTeamSummon(sessionID string, target teamSummonTarget) (dto.TeamMaterializeResult, error)` — materializeTeamSummon 把解析结果装配进会话（走团队库条目那条既有方法）。
- `func (service *Service) resolveTeamSummon(sessionID, name string) (teamSummonTarget, string, bool)` — resolveTeamSummon 解析"名字 + 附言"：名字命中团队库条目时返回目标与附言
- `func (service *Service) teamSummonIndex(sessionID string) teamSummonIndex` — teamSummonIndex 组装解析用的名字集合（团队库一次读取）。
- `func (index teamSummonIndex) match(name string) (teamSummonTarget, bool)` — match 按既有口径查名：团队库条目看 team_id/名字（team_kind 只是别名，也能命中）；
- `func teamNameCandidates(name string) []string` — teamNameCandidates 给出"名字可能是哪一段"的候选，**最长优先**：整串，以及它在
- `func presumedTeamName(name string) string` — presumedTeamName 从"名字 + 附言"里取最可能的名字（首个 token）：全部候选都没
- `func (target teamSummonTarget) displayName() string`
- `func teamSummonNotice(target teamSummonTarget, result dto.TeamMaterializeResult, tail string, record *goaldomain.GoalRecord) string` — teamSummonNotice 是装配回执：团队名 + 在编席位 + 发言顺序，并把 TeamView 的
- `func memberNames(members []dto.TeamMember) []string` — memberNames 按成员表顺序取角色名（跳过空名）。
- `func (service *Service) unknownTeamNotice(sessionID, name string) string` — unknownTeamNotice 在名字没命中任何团队时给出可行动提示。
- `func (service *Service) teamSummonHelp(sessionID string) string` — teamSummonHelp 是 `@` 的自述：**可用团队从团队库里读**（一次读取——这条不是渲染

### input_team_suggestions_repro_test.go

- `func libraryTeam(teamID, name string) dto.TeamLibraryEntry` — libraryTeam 造一条团队库条目（team_id + 展示名 + 一个员工）。
- `func (s *countingLibrarySessions) ReadTeamLibrary(sessionID string) (dto.TeamLibrary, error)`
- `func suggestionTexts(suggestions []Suggestion) []string` — suggestionTexts 取候选的 Text 列表（断言顺序无关的包含关系）。
- `func TestSuggestionsListTeamLibraryAndFollowLibraryWrites(t *testing.T)`
- `func (s *perSessionLibrarySessions) ReadTeamLibrary(sessionID string) (dto.TeamLibrary, error)`
- `func TestSuggestionsScopeTeamLibraryPerSession(t *testing.T)` — TestSuggestionsScopeTeamLibraryPerSession 钉住快照的键是**会话**：切到另一侧的
- `func TestSuggestionsTeamEmptyLibraryStaysQuiet(t *testing.T)` — TestSuggestionsTeamEmptyLibraryStaysQuiet 钉住空库与读不到的边界：`@` 整列为空

### input_team_test.go

- `func summonFixture(t *testing.T, mainHeadSeq uint64) (*librarySessions, *Service)` — summonFixture 造"会话里有一支 goal 形态团队、且已存进团队库"的现场，然后清掉会话侧
- `func summonService(t *testing.T, sessions SessionPort) *Service` — summonService 造一个带团队存储面的服务，并把视图会话钉成固定 ID
- `func noticesText(service *Service) string` — noticesText 读可见会话里的系统通知（notice 的落地通道）。
- `func waitTeamChanged(t *testing.T, subscription Subscription, sessionID string) Event` — waitTeamChanged 等一条 team.changed（超时即失败）。
- `func TestSubmitTeamPublishesTeamChanged(t *testing.T)` — TestSubmitTeamPublishesTeamChanged 钉住"召唤后面板有据可依"：装配成功必须发一条
- `func TestMaterializeAgentTeamPublishesTeamChanged(t *testing.T)` — TestMaterializeAgentTeamPublishesTeamChanged 钉住收口位置：面板 RPC「一键装配」与
- `func TestReadPathDoesNotPublishTeamChanged(t *testing.T)` — TestReadPathDoesNotPublishTeamChanged 是上一条的边界：读成员表**不**发通告。
- `func TestSubmitTeamSummonsLibraryTeam(t *testing.T)`
- `func TestSubmitTeamWithoutNameDescribesLibrary(t *testing.T)`
- `func TestSubmitTeamSummonsLibraryEntryByName(t *testing.T)`
- `func TestSubmitTeamTrailingTextIsSentAsInput(t *testing.T)` — TestSubmitTeamTrailingTextIsSentAsInput 钉住 `@<团队> <附言>`：附言不再被当成
- `func TestSubmitTeamResolvesMultiWordLibraryNameWithTrailingText(t *testing.T)` — TestSubmitTeamResolvesMultiWordLibraryNameWithTrailingText 钉住另一半：
- `func TestSubmitTeamUnknownNameWithTrailingTextReportsNameOnly(t *testing.T)` — TestSubmitTeamUnknownNameWithTrailingTextReportsNameOnly 钉住失败口径：
- `func TestSubmitTeamUnknownNameGivesActionableNotice(t *testing.T)`
- `func TestSigilMigrationHintsPointAtTheRightPrefix(t *testing.T)`
- `func TestMasterCRUDsPublishTeamChanged(t *testing.T)` — TestMasterCRUDsPublishTeamChanged 钉住「母本 CRUD 也发 team.changed」：员工库 /

### input_team_work_test.go

- `func TestSubmitTeamWithTrailingTextBeginsGoalAndTeamLeaves(t *testing.T)`
- `func TestSubmitTeamWithoutTrailingTextKeepsTeam(t *testing.T)`

### input_unknown_command_notice_test.go

- `func TestUnknownCommandNoticeSeparatesToolsFromCommands(t *testing.T)` — 未知命令 / 帮助文案的服务发现一致性。
- `func TestUnknownCommandSubmitKeepsNoticeNotError(t *testing.T)` — TestUnknownCommandSubmitKeepsNoticeNotError：未知命令仍然不是错误（照常回
- `func TestHelpStatesPanelListsOnlyExecutableEntries(t *testing.T)` — TestHelpStatesPanelListsOnlyExecutableEntries：`/help` 的口径必须与建议面板一致——
