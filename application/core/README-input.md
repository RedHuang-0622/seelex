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

- `func (service *Service) submitTeam(_ context.Context, name string) error` — submitTeam 是 `@` 前缀的落点：手动召唤一支团队到当前会话。
- `func (service *Service) materializeTeamSummon(sessionID string, target teamSummonTarget) (dto.TeamMaterializeResult, error)` — materializeTeamSummon 把解析结果装配进会话（preset 与库条目各走既有方法）。
- `func (service *Service) resolveTeamSummon(sessionID, name string) (teamSummonTarget, bool)` — resolveTeamSummon 按名字解析可召唤团队：内置形态优先（零 I/O），再查团队库。
- `func teamPresetTargets() []teamSummonTarget` — teamPresetTargets 把内置团队形态投影成召唤候选（零 I/O）。
- `func (target teamSummonTarget) displayName() string`
- `func (service *Service) publishTeamSummonChanged()` — publishTeamSummonChanged 在召唤成功后发一次快照变更事件（会话作用域），
- `func teamSummonNotice(target teamSummonTarget, result dto.TeamMaterializeResult) string` — teamSummonNotice 是装配回执：团队名 + 在编席位 + 发言顺序，并把 TeamView 的
- `func memberNames(members []dto.TeamMember) []string` — memberNames 按成员表顺序取角色名（跳过空名）。
- `func (service *Service) unknownTeamNotice(sessionID, name string) string` — unknownTeamNotice 在名字没命中任何团队时给出可行动提示。
- `func teamSummonHelp() string` — teamSummonHelp 是 `@` 的自述：内置形态逐个列出（摘要取自形态自身的事实），

### input_team_test.go

- `func summonService(t *testing.T, sessions SessionPort) *Service` — summonService 造一个带团队存储面的服务，并把视图会话钉成固定 ID
- `func noticesText(service *Service) string` — noticesText 读可见会话里的系统通知（notice 的落地通道）。
- `func TestSubmitTeamSummonsPresetTeam(t *testing.T)`
- `func TestSubmitTeamWithoutNameDescribesPresets(t *testing.T)`
- `func TestSubmitTeamSummonsLibraryEntryByName(t *testing.T)`
- `func TestSubmitTeamUnknownNameGivesActionableNotice(t *testing.T)`
- `func TestSigilMigrationHintsPointAtTheRightPrefix(t *testing.T)`
