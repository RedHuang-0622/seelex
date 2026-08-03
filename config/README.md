# Configuration

`defaults.context_window` 表示模型的总上下文窗口，`defaults.max_tokens` 表示单次响应的最大输出 token。账号条目可以覆盖这两个值；Application 会从总窗口中扣除输出预留和 12.5% 安全余量后计算可用输入预算。

## 模块定位

`config/` 存放运行时账号配置模板和本机私有配置。配置最终由 Seele ChatClient/AccountPool 读取，Seelex composition root 负责选择文件并注入 Runtime。

## 文件约定

- `accounts.example.yaml`：唯一可公开复制、文档引用和发行打包的账号模板。
- `accounts.yaml`：本机实际账号文件，可能含秘密，不应提交或出现在文档输出。
- `*.local.yaml`：机器或开发者专用覆盖文件，同样不得发布。

账号按 `subagent`、`agent`、`goalplan` 等 role 分组；缺少专用 role 时由 bridge 的 fallback 规则选择账号。

## 安全规则

- 不读取或展示真实 `api_key`、token、password、DSN。
- 新示例使用明显占位符，不使用 `sk-` 形式的伪密钥，以免触发安全扫描。
- release/CI 只能复制白名单模板。
- GUI 返回存储设置时必须使用 safe/redacted config。

## Review 指南

- 新字段是否由实际 loader 支持，而非只修改 YAML。
- role fallback 是否保持确定性，disabled account 是否被排除。
- 本机路径和秘密是否意外进入 Git diff、日志或构建产物。

## 验证

```text
go test . -run 'Config|Account' -count=1
go test ./seelebridge -run Account -count=1
```
