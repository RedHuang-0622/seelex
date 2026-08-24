# seelebridge/search

## 生态位

`seelebridge/search` 是 web_search 的**代理策略域**：提供引擎无关的统一
`Strategy` 接口、策略装配器（`Assemble`）与标准 websearch 协议策略。
主要调用方是 `seelebridge/tools/websearch`（工具侧装配点）与 `application`
门面（兼容转发）；它不直接对接 GUI/前端，也不管理账号池生命周期。

设计遵循「不绑定任何搜索引擎 + 代理策略配置 + 装配器模式」，选型依据见
[docs/research/websearch-provider-design-research-2026-08.md](../../docs/research/websearch-provider-design-research-2026-08.md)。

## 职责与非职责

职责：

- 定义 `Strategy` 接口与规范模型（`SearchResponse` / `SearchItem`）；
- `Assemble` 把配置装配为可执行策略：加载 → 校验 → 选择 → 构建；
- 标准协议策略：POST JSON + Bearer 鉴权，解析 Tavily 兼容响应
  （`results` 数组 + 可选 `answer`），端点可配置；
- 旧字段（`provider: tavily` / `api_key`）自动翻译为兼容策略；
- 从账号池 YAML 的 `websearch` 段加载配置（`LoadConfig`）；
- 把归一化结果格式化为模型可消费的 Markdown（`FormatResponse`）。

刻意不做什么：

- 不注册工具、不持有运行时依赖（`tools/websearch` 负责装配）；
- 不维护引擎注册表、不默认选择任何搜索引擎、不暴露请求/响应字段映射等
  细节参数（策略配置只有 name / endpoint / 密钥）；
- 不实现浏览器抓取/渲染，只调用搜索 API；
- 不做多策略路由/fallback 与健康检查（见调研报告的演进空间）；
- 不读取或输出真实 API key；key 只从配置注入并随请求发送。

## 目录或文件结构

| 文件 | 职责 |
|---|---|
| `strategy.go` | `Strategy` 接口、规范模型、装配器 `Assemble`、兼容入口 `WebSearch` |
| `config.go` | `WebSearchConfig` / `StrategyConfig` 配置结构，`LoadConfig` 加载与合并默认值 |
| `strategy_standard.go` | 标准 websearch 协议策略、全局默认超时与 `ApplyLimits` |
| `format.go` | 规范结果 → Markdown 输出 |
| `*_test.go` | 装配、配置、标准策略与格式化测试 |

## 核心实现

### 接口与规范模型

```go
type Strategy interface {
	Name() string
	Search(ctx context.Context, query string, maxResults int) (SearchResponse, error)
}
```

`SearchResponse` 由 `Answer`（AI 摘要）与 `Items`（`SearchItem`：标题/URL/摘要/分数）组成。

### 装配器（Assembler）

`Assemble(cfg)` 是策略域唯一装配点，选择规则：

1. 优先使用 `cfg.Strategies`（代理策略列表）；
2. 未声明策略但存在旧字段（`provider: tavily` / `api_key`）时，自动翻译为
   内置 tavily 兼容策略；
3. `active`（或旧 `provider` 名）用于多策略中选择，缺省取第一个；
4. 没有任何策略时返回错误，由工具层注册占位工具。

### 标准协议策略

`standardStrategy` 只依赖 `StrategyConfig{name, endpoint, api_key}`：

- 请求：POST `endpoint`，JSON body 为 `{query, search_depth, max_results, include_answer}`，
  `Authorization: Bearer <api_key>`；
- 响应：按 Tavily 兼容格式解析 `answer` 与 `results[]`（title/url/content/score）；
- 未配置 key 或 endpoint 时装配期报错；旧字段未声明 endpoint 时沿用默认
  `https://api.tavily.com/search`。

### 配置加载

`LoadConfig` 从账号池 YAML 的 `websearch` 段加载并合并默认值（max_results=5、
include_answer=true、search_depth=advanced）；文件缺失或解析失败返回默认值。
策略密钥同时接受 `api_key` 与 `apikey` 两种写法；`include_answer` 仅在显式
配置时覆盖。

## 数据流或生命周期

```text
账号池 YAML ──LoadConfig──▶ WebSearchConfig ──Assemble──▶ Strategy
                                                            │ Search(ctx, query, maxResults)
web_search 工具 handler ────────────────────────────────────┤
                                                            ▼
                                              SearchResponse（规范模型）
                                                            │ FormatResponse
                                                            ▼
                                                  Markdown 返回给模型
```

装配发生在启动期（`tools/websearch.Register`）；每个搜索请求独立执行：
策略内 `http.Client` 每次 `Search` 只发一次请求，继承 caller context；
无共享可变状态，策略对象可在多次调用间复用。

## 依赖方向

- 允许依赖：标准库 `net/http` / `encoding/json`、`gopkg.in/yaml.v3`；
- 禁止反向依赖：不依赖 `seelebridge` 根包、工具注册层、GUI、`application` 或 Engine；
- 工具层（`tools/websearch`）只能通过 `Strategy` 接口消费，不复制业务状态机。

## 并发、存储、安全或错误语义

- 并发：策略无共享可变状态，可安全并发调用；
- 存储：本包不持久化任何数据；
- 安全：API key 只存在于配置与请求头中，绝不写入日志、Snapshot 或 README；
  示例只引用 `config/accounts.example.yaml` 的占位符风格；
- 错误语义：装配期错误说明「未配置策略 / 未找到策略 / 缺少 endpoint / 缺少 key」；
  请求期错误带 `web_search:` 前缀；非 2xx 附带响应体片段便于排障；
  context 取消能中断请求；
- 结果数上限：`limitResults` 把请求结果数限制在 1..`DefaultMaxResults`（10），
  超限回退配置默认值，避免异常大响应。

## 扩展方式

接入新的搜索 API/代理：**无需改代码**，在账号池 `websearch.strategies`
声明 `name / endpoint / 密钥` 即可，端点需兼容标准 websearch 协议
（Tavily 兼容）；非标准协议可自建适配网关，或在 `Assemble` 增加策略类型。

新增策略类型（如非标准协议）：

1. 新增 `xxx.go`，实现 `Strategy` 接口；
2. 在 `Assemble` 增加类型分发；
3. 在 README 配置示例与调研报告「演进空间」同步；
4. 补充装配与请求测试。

## Review 指南

- 是否仍存在「默认引擎 / 引擎注册表」的隐性耦合？新策略应只由配置驱动；
- `Assemble` 的旧字段翻译是否会误吞新配置（如 strategies 与 api_key 并存时）？
- 策略配置错误（缺 endpoint、缺 key）是否在装配期提前拦截？
- `LoadConfig` 的默认合并是否会被零值误覆盖（尤其 include_answer）？
- API key 是否可能进入日志或返回给模型？
- 超时：`websearch.timeout` 与全局 `search_timeout`（旧名 `tavily_timeout`）的优先级是否清晰？

## 测试与验证

```text
go test ./seelebridge/search/... -count=1
go test ./seelebridge/tools/websearch/... -count=1
go test ./seelexctx/... -count=1
```

关键测试文件：

- `strategy_test.go`：无策略报错、旧字段翻译、最简策略装配、active 选择、缺 endpoint；
- `config_test.go`：默认值、部分/全量覆盖、include_answer 显式覆盖、strategies 与 apikey 别名加载；
- `strategy_standard_test.go`：httptest 验证请求体/鉴权/归一化、结果数上限、API 错误、
  context 取消、格式化。

## 文件与函数索引

### strategy.go

```go
type SearchItem struct { Title, URL, Content string; Score float64 }
type SearchResponse struct { Answer string; Items []SearchItem }
type Strategy interface { Name() string; Search(ctx context.Context, query string, maxResults int) (SearchResponse, error) }
const DefaultMaxResults = 10
func Assemble(cfg WebSearchConfig) (Strategy, error)                 // 装配器：选择并构建代理策略
func legacyTavilyStrategy(cfg WebSearchConfig) (StrategyConfig, bool) // 旧字段翻译为兼容策略
func strategyNames(strategies []StrategyConfig) string               // 返回可用策略名列表
func limitResults(defaultMax, requested int) int                     // 归一化请求结果数
func WebSearch(ctx context.Context, cfg WebSearchConfig, query string, maxResults int) (string, error) // 保留的兼容入口
```

### config.go

```go
type WebSearchConfig struct { /* provider/api_key/endpoint/max_results/include_answer/search_depth/timeout/active/strategies */ }
type StrategyConfig struct { Name, Endpoint, APIKey, APIKeyAlias string }
func DefaultConfig() WebSearchConfig                                 // 返回 websearch 的默认配置
func LoadConfig(accountsPath string) WebSearchConfig                 // 从账号池 YAML 加载 websearch 段
func mergeConfig(dst *WebSearchConfig, src WebSearchConfig)          // 用加载值覆盖非零字段
```

### strategy_standard.go

```go
const defaultTavilyEndpoint = "https://api.tavily.com/search"
var DefaultTimeout = 15 * time.Second
func ApplyLimits(searchTimeoutSec int)                               // 注入 seele.yaml limits 中 search 相关配置
func providerTimeout(timeoutSeconds int) time.Duration               // 返回策略超时
type standardStrategy struct { name, endpoint, apiKey string; opts WebSearchConfig; client *http.Client }
func newStandardStrategy(cfg StrategyConfig, opts WebSearchConfig) (Strategy, error) // 构建标准协议策略
func (p *standardStrategy) Name() string                             // 返回策略名称
func (p *standardStrategy) Search(ctx context.Context, query string, maxResults int) (SearchResponse, error) // 按标准协议调用搜索端点并归一化
type tavilyResponse struct { Answer string; Results []tavilyResult }
type tavilyResult struct { Title, URL, Content string; Score float64 }
```

### format.go

```go
func FormatResponse(resp SearchResponse) string                     // 规范结果格式化为 Markdown
```

### 测试文件

```go
strategy_test.go:        TestAssemble_NoStrategies / TestAssemble_LegacyTavilyWithoutKey /
                         TestAssemble_LegacyTavilyOnlyAPIKey / TestAssemble_MinimalStrategy /
                         TestAssemble_ActiveSelection / TestAssemble_ActiveNotFound /
                         TestAssemble_MissingEndpoint / TestLimitResults / TestWebSearch_NoStrategy
config_test.go:          TestLoadConfig_Defaults / TestLoadConfig_InvalidYAML /
                         TestLoadConfig_PartialOverride / TestLoadConfig_FullOverride /
                         TestLoadConfig_EmptyAPIKeyKeepsDefault / TestLoadConfig_ZeroMaxResultsKeepsDefault /
                         TestLoadConfig_Strategies / TestLoadConfig_StrategiesApikeyAlias
strategy_standard_test.go: TestStandardStrategy_Search / TestStandardStrategy_MaxResultsBounds /
                         TestStandardStrategy_APIError / TestStandardStrategy_ContextCancelled /
                         TestFormatResponse_WithAnswer / TestFormatResponse_NoAnswer /
                         TestFormatResponse_EmptyResults / TestFormatResponse_ResultsWithoutContent
```
