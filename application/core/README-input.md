# core/input（根包分卷）

## 生态位

输入分派与路由兼容测试

覆盖：`input*.go`；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

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
- `func (service *Service) materializeTeamSummon(sessionID string, target teamSummonTarget) (dto.TeamMaterializeResult, error)` — materializeTeamSummon 把解析结果装配进会话（preset 与库条目各走既有方法）。
- `func (service *Service) resolveTeamSummon(sessionID, name string) (teamSummonTarget, string, bool)` — resolveTeamSummon 解析"名字 + 附言"：名字命中内置形态或团队库条目时返回目标与
- `func (service *Service) teamSummonIndex(sessionID string) teamSummonIndex` — teamSummonIndex 组装解析用的名字集合（内置形态优先，库条目一次读取）。
- `func (index teamSummonIndex) match(name string) (teamSummonTarget, bool)` — match 按既有口径查名：内置形态看 team_id/team_kind，库条目看 team_id/名字；
- `func teamNameCandidates(name string) []string` — teamNameCandidates 给出"名字可能是哪一段"的候选，**最长优先**：整串，以及它在
- `func presumedTeamName(name string) string` — presumedTeamName 从"名字 + 附言"里取最可能的名字（首个 token）：全部候选都没
- `func teamPresetTargets() []teamSummonTarget` — teamPresetTargets 把内置团队形态投影成召唤候选（零 I/O）。
- `func (target teamSummonTarget) displayName() string`
- `func teamSummonNotice(target teamSummonTarget, result dto.TeamMaterializeResult, tail string) string` — teamSummonNotice 是装配回执：团队名 + 在编席位 + 发言顺序，并把 TeamView 的
- `func memberNames(members []dto.TeamMember) []string` — memberNames 按成员表顺序取角色名（跳过空名）。
- `func (service *Service) unknownTeamNotice(sessionID, name string) string` — unknownTeamNotice 在名字没命中任何团队时给出可行动提示。
- `func teamSummonHelp() string` — teamSummonHelp 是 `@` 的自述：内置形态逐个列出（摘要取自形态自身的事实），

### input_team_test.go

- `func summonService(t *testing.T, sessions SessionPort) *Service` — summonService 造一个带团队存储面的服务，并把视图会话钉成固定 ID
- `func noticesText(service *Service) string` — noticesText 读可见会话里的系统通知（notice 的落地通道）。
- `func waitTeamChanged(t *testing.T, subscription Subscription, sessionID string) Event` — waitTeamChanged 等一条 team.changed（超时即失败）。
- `func TestSubmitTeamPublishesTeamChanged(t *testing.T)` — TestSubmitTeamPublishesTeamChanged 钉住"召唤后面板有据可依"：装配成功必须发一条
- `func TestMaterializeAgentTeamPublishesTeamChanged(t *testing.T)` — TestMaterializeAgentTeamPublishesTeamChanged 钉住收口位置：goal 上线自动装配与
- `func TestReadPathDoesNotPublishTeamChanged(t *testing.T)` — TestReadPathDoesNotPublishTeamChanged 是上一条的边界：读成员表**不**发通告。
- `func TestSubmitTeamSummonsPresetTeam(t *testing.T)`
- `func TestSubmitTeamWithoutNameDescribesPresets(t *testing.T)`
- `func TestSubmitTeamSummonsLibraryEntryByName(t *testing.T)`
- `func TestSubmitTeamTrailingTextIsSentAsInput(t *testing.T)` — TestSubmitTeamTrailingTextIsSentAsInput 钉住 `@<团队> <附言>`：附言不再被当成
- `func TestSubmitTeamResolvesMultiWordLibraryNameWithTrailingText(t *testing.T)` — TestSubmitTeamResolvesMultiWordLibraryNameWithTrailingText 钉住另一半：
- `func TestSubmitTeamUnknownNameWithTrailingTextReportsNameOnly(t *testing.T)` — TestSubmitTeamUnknownNameWithTrailingTextReportsNameOnly 钉住失败口径：
- `func TestSubmitTeamUnknownNameGivesActionableNotice(t *testing.T)`
- `func TestSigilMigrationHintsPointAtTheRightPrefix(t *testing.T)`
