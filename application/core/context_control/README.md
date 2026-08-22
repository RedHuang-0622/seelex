# context_control

## 生态位

`seele.yaml` window 配置段加载与窗口策略类型：`WindowConfig`/`WindowPolicy`/
`LoadWindowConfig`。决策公式归属 seelexctx，本包不做实现。

## 测试

根包 `window_policy_test.go` 覆盖。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### window_policy.go

- `func NewDefaultWindowPolicy(config WindowConfig) DefaultWindowPolicy` — NewDefaultWindowPolicy 从 window 配置段构建策略。
- `func DefaultWindowConfig() WindowConfig` — DefaultWindowConfig 返回确认点 5 的既定默认值（ratio/min_rounds/max_rounds）。
- `func LoadWindowConfig(path string) (WindowConfig, error)` — LoadWindowConfig 读取 seele.yaml 的 window 配置段（路径门控同款加载风格）。

### window_policy_test.go

- `func TestDefaultWindowPolicyFormulaClamps(t *testing.T)`
- `func TestDefaultWindowPolicyConfigOverrideWins(t *testing.T)`
- `func TestDefaultWindowPolicyFallsBackToMinRounds(t *testing.T)`
- `func TestDefaultWindowPolicyUsesConfigRatioAndBounds(t *testing.T)`
- `func TestDefaultWindowConfigDecidedDefaults(t *testing.T)`
- `func TestLoadWindowConfigParsesSection(t *testing.T)`

