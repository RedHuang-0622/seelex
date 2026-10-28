# test/permission（集成用例分卷）

## 生态位

权限门与审批桥：配置叠加、员工面板、真 API 权限冒烟。

覆盖：`permission_*.go`；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### permission_config_test.go

- `func mergedPermissionConfig(t *testing.T) toolspermission.PermissionConfig` — mergedPermissionConfig 复刻组合根的装配：默认权责配置 + config/seele.yaml 覆盖。
- `func TestEveryRegisteredToolIsRouted(t *testing.T)` — TestEveryRegisteredToolIsRouted 钉住"任一工具都能被机械归组"：没有未分封漏点，
- `func TestMainAgentToolDecisions(t *testing.T)` — TestMainAgentToolDecisions 是主代理逐工具判定表（root 主体）。
- `func TestMainAgentBashArgPolicy(t *testing.T)` — TestMainAgentBashArgPolicy 钉住 bash 的 args 级能力白名单：命令本体参与模式
- `func TestMergedConfigSubagentAndEmployee(t *testing.T)` — TestMergedConfigSubagentAndEmployee 走真实合并配置验收两条产品口径：

### permission_employee_panel_test.go

- `func TestEmployeeEscalationLandsInOwnerSessionPanel(t *testing.T)` — TestEmployeeEscalationLandsInOwnerSessionPanel 走真实组合根桥接（boot.NewPermissionBridge）
