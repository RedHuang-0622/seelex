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
- `func (service *Service) endSkill() error`
- `func (service *Service) activateSkillAndSubmit(ctx context.Context, skill SkillInfo, args []string, input string) error`
- `func (service *Service) prepareCompletedTaskBoundary()`
- `func (service *Service) applySkill(skill SkillInfo)`

### input_router_compat_test.go

- `func TestNewAssemblesInputRouter(t *testing.T)`
