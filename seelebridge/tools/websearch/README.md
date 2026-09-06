# Websearch（工具装配点）

## 生态位

`seelebridge/tools/websearch` 是 `web_search` 工具的**装配点（Assembler）**：
从账号池 YAML 的 `websearch` 段加载配置，由 `search.Assemble` 装配出代理
策略，再注册为 `web_search` 工具。主要调用方是 composition root（`main.go`）。

## 职责与非职责

职责：

- 通过窄接口 `ToolRegistrar` 注册 `web_search`（避免反向依赖 `seelebridge` 根包）；
- 配置加载 → 策略装配 → 工具注册的装配流程；
- 没有可用策略时注册占位工具并给出 `websearch.strategies` 修复指引。

刻意不做什么：

- 不实现搜索逻辑、不解析响应（属于 `seelebridge/search`）；
- 不决定默认引擎、不维护策略实现（引擎无关由 search 域保证）。

## 核心实现

`Register(registrar, accountsPath)` 流程：

1. `search.LoadConfig(accountsPath)` 加载并合并默认值；
2. `search.Assemble(cfg)` 装配代理策略（旧 `provider: tavily` / `api_key`
   自动兼容，含内置厂商 `bochaai` 等；`strategies[].type` 指定厂商适配器）；
3. 装配失败 → 注册占位工具（返回错误 JSON，不 panic）；
4. 成功 → handler 解析 `query` / `max_results` 参数，调用 `strategy.Search`
   并 `FormatResponse` 输出 Markdown。

## 数据流或生命周期

```text
main.go ── websearch.Register(runtime, accountsPath)
            │ LoadConfig → Assemble
            ▼
        strategy（search.Strategy）
            │ web_search handler: args → Search → FormatResponse
            ▼
        Markdown 返回给模型
```

装配发生在启动期一次；请求期间策略对象无共享可变状态，可并发调用。

## 依赖方向

- 允许依赖：`seelebridge/search`（仅通过 `Strategy` 接口消费）；
- 禁止反向依赖：不依赖 `seelebridge` 根包、GUI、`application` 或 Engine。

## 并发、存储、安全或错误语义

- 并发：handler 由运行时按工具调度调用，策略无状态可并发；
- 存储：不持久化数据；
- 安全：不输出 API key；占位工具的错误信息只给配置指引；
- 错误语义：参数 JSON 非法或 query 为空返回带 `web_search:` 前缀的错误；
  搜索失败原样透传（已含策略名上下文）。

## 扩展方式

- 接入新搜索 API：只改账号池 `websearch.strategies`（标准协议端点），
  本包无需改动；
- 新增内置厂商：在 `seelebridge/search` 新增 `builtin_xxx.go` 并
  `registerBuiltin`（装配逻辑零改动），本包仅更新占位提示文案（错误信息
  已动态列出可用厂商）与测试。

## Review 指南

- handler 是否仍只依赖 `Strategy` 接口，没有复制业务状态？
- 占位工具是否在任何装配错误下都能注册（不 panic）？
- `query` 空值与 `max_results` 越界是否被正确处理？
- 占位错误文案是否仍只给配置指引（不泄漏 key / 不重复厂商实现细节）？

## 测试与验证

```text
go test ./seelebridge/tools/websearch -count=1
```

关键测试文件：`websearch_test.go`（占位工具、自定义策略端到端、空 query、schema）。
