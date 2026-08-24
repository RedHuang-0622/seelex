# websearch 通用接入设计模式调研

日期：2026-08-24
状态：调研结论（已按结论完成重构，见 [seelebridge/search](../../seelebridge/search/README.md)）
范围：`web_search` 工具从「硬编码单一供应商」演进为「不指定引擎、直接用 websearch 搜索；通过代理策略配置与装配器模式做通用 API 接入」时的设计模式选型。

## 结论（TL;DR）

采用 **引擎无关（Engine-agnostic）+ 代理策略配置（Proxy Strategy Config）+ 装配器（Assembler）** 的组合：

1. **引擎无关**：工具代码不感知、不注册、不默认任何具体搜索引擎。`web_search` 只依赖一个统一策略接口（`Strategy.Search(ctx, query, maxResults)`）。
2. **代理策略配置（最小面）**：每个搜索端点的接入只需声明 `name / endpoint / 密钥`；请求与响应遵循**标准 websearch 协议**（POST JSON + Bearer 鉴权，响应含 `results` 数组与可选 `answer`，即 Tavily 兼容格式）。工具本身不暴露 method、header、响应字段映射等细节参数——接入方把非标准 API 收敛到标准协议即可（自建适配网关），这是刻意保持的最简契约。
3. **装配器（Assembler）**：`search.Assemble(cfg)` 是唯一装配点：加载配置 → 校验 → 选择策略 → 构建可执行 `Strategy`；`tools/websearch.Register` 是工具侧装配点，把策略注册为 `web_search` 工具。两者共同构成「装配件模式」。
4. **兼容模板**：旧 `provider: tavily` + `api_key` 字段自动翻译为内置 tavily 兼容策略，现有账号池配置无需修改即可继续使用；tavily 只是可选的兼容路径，不是默认引擎。

## 背景与现状

重构前 `web_search` 工具：

- `seelebridge/search` 硬编码 Tavily Search API（`https://api.tavily.com/search`），配置只有 provider/api_key/max_results/include_answer/search_depth 五个字段；
- `seelebridge/tools/websearch` 从账号池 YAML 的 `websearch` 段读配置并注册工具；
- 换搜索源（如自建 SearXNG、Brave、Bing）必须改 Go 代码重新发布。

用户诉求：**不指定任意一个 websearch 引擎，直接用 websearch 引擎做搜索**——工具与引擎解耦，接入哪个搜索 API 由「代理策略配置」决定，整体用「装配器模式」完成通用 websearch API 接入。调研期间曾设计「响应字段映射全声明」的更细方案，用户明确收敛为最简契约（只需端点与密钥），因此最终实现只保留标准协议 + 可配置端点。

## 候选设计模式对比

| 方案 | 核心思想 | 优点 | 缺点 | 适用场景 |
|---|---|---|---|---|
| 现状硬编码 | 一个函数调一个端点 | 实现最简单 | 零扩展性、绑定单一引擎 | 已淘汰 |
| 纯 Strategy 策略模式 | 统一接口 + 可替换实现 | 工具层与实现解耦 | 仍需要为每个引擎写实现代码 | 作为骨架 |
| 纯 Adapter 适配器模式 | 供应商专有协议 → 规范模型 | 商业 API 差异被隔离 | 每接一个源仍要发版 | 只适合少数重量级商业 API |
| 引擎注册表（Registry/Factory） | 按引擎名登记/选择实现 | 运行时可按名选中实现 | 绑定「引擎」概念，违背引擎无关诉求 | 不采用 |
| 代理策略配置（最小面） | 只声明端点/密钥，协议固定 | 配置极简、引擎无关 | 非标准协议需自建适配网关 | 核心，本次落地 |
| 装配器（Assembler/Composition） | 配置 → 组件校验 → 组装为可执行策略 | 装配点唯一、错误前置、可测 | 需要清晰的配置契约 | 核心，连接配置与工具 |
| 插件式（Plugin 注册） | 独立模块动态注册 | 最灵活、可独立交付 | 生命周期/安全负担大 | 暂不需要 |

## 行业参考

- **thane-ai-agent**（Go）：`internal/search` 提供「pluggable web search interface」，每个搜索后端实现 `Provider` 接口，`Name()` 返回 `"searxng"`、`"brave"` 等标识 —— 印证「统一接口 + 可插拔后端」的做法；本仓库进一步把接入下沉为「端点 + 密钥」配置，做到引擎无关。
  <https://github.com/nugget/thane-ai-agent/blob/v0.8.1/internal/search/search.go>
- **jBOM**（issue #84）：明确讨论「Provider registry and config-driven provider instantiation」，指出「一个供应商 = 一种搜索 API 模式」的假设是错的 —— 印证「配置驱动实例化」是社区共识。
  <https://github.com/plocher/jBOM/issues/84>
- **LibreChat**（webSearch YAML）：`webSearch` 段可配置 providers/scrapers/rerankers，URL 与 key 集中管理 —— 印证「账号级 YAML 集中管理搜索服务」的配置形态；其协议同样默认 Tavily 兼容。
  <https://raw.githubusercontent.com/LibreChat-AI/librechat.ai/main/content/docs/configuration/librechat_yaml/object_structure/web_search.mdx>
- **omniserp**（Go SDK）：对 Serper/SerpAPI/Brave/Exa 提供统一 client 抽象 —— 印证「统一协议/统一 client」的归一化思路。
  <https://github.com/plexusone/omniserp>
- **web-researcher-mcp**：支持多 provider 与 `SEARCH_ROUTING` 优先级/fallback —— 说明「多源选择」之上还可演进「路由与容灾」，本次不实现但保留演进空间。
  <https://pkg.go.dev/github.com/zoharbabin/web-researcher-mcp>

## 落地设计

### 分层与模式落点

```text
composition root (main.go)
   │  websearch.Register(runtime, accountsPath)
   ▼
seelebridge/tools/websearch        ← 工具侧装配器：LoadConfig → Assemble → RegisterTool
   │  search.LoadConfig → search.Assemble
   ▼
seelebridge/search                 ← 策略域：引擎无关 + 装配器
   ├── Strategy 接口               ← 工具层唯一依赖
   ├── standardStrategy            ← 标准协议：POST JSON + Bearer，端点可配置
   ├── legacy tavily 翻译          ← 旧字段兼容（非默认引擎）
   └── FormatResponse              ← 统一 Markdown 输出
```

### 配置 Schema（账号池 YAML `websearch` 段）

```yaml
websearch:
  max_results: 5
  include_answer: true
  search_depth: advanced
  # timeout: 15             # 可选，覆盖 limits.search_timeout
  # active: my-search       # 可选：多策略时选中一个；缺省取第一个
  strategies:               # 代理策略列表（最简契约：端点 + 密钥）
    - name: my-search
      endpoint: https://your-search-api.example/search
      apikey: xxxxxxxx
```

规则：

- 请求：POST `endpoint`，JSON body `{query, search_depth, max_results, include_answer}`，
  `Authorization: Bearer <key>`；
- 响应：Tavily 兼容格式（`answer` 可选 + `results[]`：title/url/content/score）；
- 密钥字段接受 `api_key` 与 `apikey` 两种写法；
- 未配置 key/endpoint、`active` 未命中时装配期报错；无任何策略时注册占位工具。

旧配置自动兼容：

```yaml
websearch:
  provider: tavily            # 旧字段：自动翻译为兼容策略
  api_key: replace-with-your-tavily-api-key
  max_results: 5
```

## 兼容性策略

- 未声明 `strategies` 时，旧字段 `provider: tavily` / `api_key` 自动翻译为兼容策略，现有配置零改动；
- `application` 门面的 `WebSearchConfig` / `WebSearch` 别名与转发保持不变；
- 超时字段保留旧名 `tavily_timeout` 作为别名，新名 `search_timeout` 优先（见 [seelexctx/limits.go](../../seelexctx/limits.go)）；
- `include_answer` 加载语义顺带修复：未显式配置时保持默认 true，不再被无条件覆盖为 false。

## 演进空间（本次未实现）

- 多策略路由/fallback（如 `SEARCH_ROUTING`）：可在 `Strategy` 之上加组合策略；
- 非标准协议接入：新增策略类型（如 OAuth 签名、字段映射），由 `Assemble` 统一装配；
- 策略健康检查与连通性验证（装配时可提供 dry-run）；
- 插件式策略注册：未来如需要，可把策略构建器提升为公开装配 API。

## 验证

```text
go test ./seelebridge/search/... ./seelebridge/tools/websearch/... -count=1
go test ./seelexctx/... -count=1
go test ./e2e/ -count=1
```

关键测试文件：`seelebridge/search/strategy_test.go`、`strategy_standard_test.go`、`config_test.go`、`seelebridge/tools/websearch/websearch_test.go`。
