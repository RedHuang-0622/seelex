# Application Search

## 定位

本包实现独立的 Tavily Web Search 调用与文本格式化，供 composition root 注册为 Agent 工具。它不是通用浏览器，也不管理账号池。

## 实现

`WebSearchConfig` 包含 API key、endpoint、结果数量、answer/raw-content 等选项。`WebSearch` 校验 query 和 `maxResults`，构造 HTTP 请求，继承 caller context，并把 Tavily 响应格式化为适合模型消费的文本。

默认 timeout 由 HTTP client 控制；context cancel 必须能中断请求。响应读取和 JSON 解析错误带有 search 上下文。

## 安全边界

- API key 只从运行配置注入，不写入 Snapshot、日志或 README。
- 示例只引用环境变量或 `config/accounts.example.yaml` 风格占位符。
- 外部响应是不可信文本，只作为工具结果返回。

## Review 指南

- max results 是否有上下界，避免意外大响应。
- 非 2xx、空结果、缺失 content 和 context cancel 是否有稳定语义。
- 不要在此包引入 GUI 或 Engine 依赖。

## 测试

```text
go test ./application/search -count=1
```
