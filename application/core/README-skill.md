# core/skill

## 生态位

Skill 指令信封编解码

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### skill_activate.go

- `func (service *Service) ActivateSkill(name string) (SkillInfo, error)` — ActivateSkill 把当前插件的一个技能激活进当前会话的 skill 层（模型侧

### skill_activate_test.go

- `func TestActivateSkillPushesLayerAndNextSubmissionTrustsIt(t *testing.T)` — TestActivateSkillPushesLayerAndNextSubmissionTrustsIt：模型侧激活（ActivateSkill）
- `func TestActivateSkillIdempotent(t *testing.T)` — TestActivateSkillIdempotent：同名重复激活不产生重复层（Push 同名覆盖）。
- `func TestActivateSkillRejectsBadNames(t *testing.T)` — TestActivateSkillRejectsBadNames：空名/未知技能/goal 均拒绝且不改状态。
- `func TestActivateSkillCoexistsWithUserSlashSkill(t *testing.T)` — TestActivateSkillCoexistsWithUserSlashSkill：模型激活与 #<name> 路径共享

### skill_context.go

- `func chatRequestDisplays(requests []chatRequest) []string`
- `func newChatRequest(input string, layers []PromptLayer) chatRequest`
- `func selectedSkillLayers(layers []PromptLayer) []PromptLayer`
- `func formatSkillUserInput(layers []PromptLayer, input string) string`
- `func writeSkillItem(builder *strings.Builder, skill PromptLayer)`
- `func wrapModelInput(displayInput, body string) string`
- `func displayUserInput(modelInput string) string`
- `func combineChatRequests(requests []chatRequest) chatRequest`
- `func mergeSkillLayers(current, incoming []PromptLayer) []PromptLayer`
- `func parseModelEnvelope(input string) (string, int, bool)`

### skill_context_test.go

- `func TestFormatSkillUserInputKeepsPlainInputUnchanged(t *testing.T)`
- `func TestSkillSelectionKeepsUserInputPlainAndFreezesTrustedLayers(t *testing.T)`
- `func TestDisplayUserInputRejectsInvalidEnvelope(t *testing.T)`
- `func TestAdaptEngineMessageRestoresOriginalUserInput(t *testing.T)`
- `func TestDisplayUserInputHidesPrivateRecoveryEnvelopes(t *testing.T)`
- `func TestCombineChatRequestsPreservesDisplayAndFrozenSkillLayers(t *testing.T)`
