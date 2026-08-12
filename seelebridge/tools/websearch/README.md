# Websearch

`seelebridge/tools/websearch` 提供 `web_search` 工具注册与账号池配置加载。
它通过窄接口 `ToolRegistrar`（仅 `RegisterTool`）与运行时解耦，避免反向
依赖 `seelebridge` 根包。配置从账号池 YAML 的 `websearch` 段加载，
未配置 API Key 时注册占位工具。

## 验证

```text
go test ./seelebridge/tools/websearch -count=1
```
