# Config

`seelebridge/internal/config` 承载简化账号 YAML（accounts*.yaml 角色分组格式）
的加载：产出账号规格（`model.AccountSpec`）与 Seelex 侧上下文/输出预算
（`Config`/`AccountLimits`）。属于根 facade 的装配细节（仅 runtime.go 使用），
置于 internal/；根包经 `config_aliases.go` 重导出 `accountLimits` 与默认预算
常量保持兼容。

## 验证

```text
go test ./seelebridge -count=1
```
