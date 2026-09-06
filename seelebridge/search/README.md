# seelebridge/search

## 生态位

`seelebridge/search` 是 web_search 的**代理策略域**：提供引擎无关的统一
`Strategy` 接口、策略装配器（`Assemble`）、标准 websearch 协议策略与
**内置厂商适配器**（tavily / bochaai / searxng…）。主要调用方是
`seelebridge/tools/websearch`（工具侧装配点）与 `application` 门面（兼容
转发）；它不直接对接 GUI/前端，也不管理账号池生命周期。

设计遵循「引擎无关（配置驱动）+ 代理策略配置 + 内置厂商适配器 + 装配器
模式」：工具层只依赖统一 `Strategy` 接口；厂商差异要么收敛到标准协议端点
（自建网关零代码），要么收敛到内置适配器（注册式扩展，配置只需
`type` 指定厂商名）。选型依据见
[docs/research/websearch-provider-design-research-2026-08.md](../../docs/research/websearch-provider-design-research-2026-08.md)。

## 职责与非职责

职责：

- 定义 `Strategy` 接口与规范模型（`SearchResponse` / `SearchItem`）；
- `Assemble` 把配置装配为可执行策略：加载 → 校验 → 选择 → 构建；
- 标准协议策略：POST JSON + Bearer 鉴权，解析 Tavily 兼容响应
  （`results` 数组 + 可选 `answer`），端点可配置；
- 内置厂商适配器（注册表 + 厂商工厂）：厂商专有协议（如博查 / SearXNG）
  的请求构造与响应解析各自实现，**入参/出参与标准策略完全一致**，由
  `strategies[].type`（或旧 `provider` 名）分发装配；
- 旧字段（`provider: tavily` / `api_key`）自动翻译为兼容策略；provider
  命中内置厂商名（如 bochaai）时翻译为对应厂商适配器；
- 从账号池 YAML 的 `websearch` 段加载配置（`LoadConfig`）；
- 把归一化结果格式化为模型可消费的 Markdown（`FormatResponse`）。

刻意不做什么：

- 不把任何搜索引擎设为默认：`DefaultConfig` 引擎无关（空 Provider），未装配
  任何策略时注册占位工具；旧字段兼容路径（tavily）除外；
- 不暴露请求/响应字段映射等细节参数（策略配置只有 name / type / endpoint /
  密钥；厂商专有映射固化在各自适配器内，配置无需感知）；
- 不实现浏览器抓取/渲染，只调用搜索 API；
- 不做多策略路由/fallback 与健康检查（见调研报告的演进空间）；
- 不读取或输出真实 API key；key 只从配置注入并随请求发送。

## 目录或文件结构

| 文件 | 职责 |
|---|---|
| `strategy.go` | `Strategy` 接口、规范模型、装配器 `Assemble`、兼容入口 `WebSearch`、旧字段翻译 `legacyStrategy` |
| `config.go` | `WebSearchConfig` / `StrategyConfig` 配置结构（含 `type`），`LoadConfig` 加载与合并默认值 |
| `strategy_standard.go` | 标准 websearch 协议策略、全局默认超时与 `ApplyLimits` |
| `builtin.go` | 内置厂商注册表与 `type` 分发（`assembleStrategy`） |
| `builtin_tavily.go` | 内置 tavily 适配器（原生即标准协议，复用 standardStrategy） |
| `builtin_bocha.go` | 内置博查（BochaAI）适配器：专有 body / 响应归一化 |
| `builtin_searxng.go` | 内置 SearXNG 适配器：GET `format=json` / 响应归一化 |
| `format.go` | 规范结果 → Markdown 输出 |
| `*_test.go` | 装配、配置、标准策略、内置厂商与格式化测试 |

## 核心实现

### 接口与规范模型

```go
type Strategy interface {
	Name() string
	Search(ctx context.Context, query string, maxResults int) (SearchResponse, error)
}
```

`SearchResponse` 由 `Answer`（AI 摘要）与 `Items`（`SearchItem`：标题/URL/摘要/分数）组成。
无论哪种策略（标准协议或内置厂商），**入参与出参都是这个契约**，切换厂商只改配置。

### 装配器（Assembler）

`Assemble(cfg)` 是策略域唯一装配点，选择规则：

1. 优先使用 `cfg.Strategies`（代理策略列表）；
2. 未声明策略但存在旧字段时自动翻译为单条策略：`provider: tavily`（或空
   provider + `api_key`）→ tavily 兼容策略；provider 命中内置厂商名
   （bochaai / searxng…）→ 对应厂商适配器（旧账号池配置零改动）；
3. `active`（或旧 `provider` 名）用于多策略中选择，缺省取第一个；
4. 单条策略按 `strategies[].type` 分发（见下「内置厂商适配器」）；
5. 没有任何策略时返回错误（提示可用内置厂商），由工具层注册占位工具。

### 标准协议策略

`standardStrategy` 只依赖 `StrategyConfig{name, type, endpoint, api_key}`（type
为空/standard 时）：

- 请求：POST `endpoint`，JSON body 为 `{query, search_depth, max_results, include_answer}`，
  `Authorization: Bearer <api_key>`；
- 响应：按 Tavily 兼容格式解析 `answer` 与 `results[]`（title/url/content/score）；
- 未配置 key 或 endpoint 时装配期报错；未声明 endpoint 时沿用默认
  `https://api.tavily.com/search`。

### 内置厂商适配器（Builtin vendor adapters）

「代理策略配置」解决 Tavily 兼容端点；**内置厂商适配器**解决专有协议厂商
（无需自建网关）：各厂商一个 `Strategy` 实现，`strategies[].type` 或旧
`provider` 命中注册表即被装配，对外契约与标准策略一致。

```yaml
websearch:
  strategies:
    - name: bocha            # type: bochaai → 博查原生 API（Bearer + 专有 body）
      type: bochaai
      api_key: sk-xxx
    - name: sx               # type: searxng → SearXNG JSON API（GET format=json）
      type: searxng
      endpoint: https://searx.example.org/search   # 必填；api_key 可选
    # 省略 type（或 standard）→ 标准 Tavily 兼容协议（POST JSON + Bearer）
    - name: gateway
      endpoint: https://your-gateway.example/search
      api_key: xxxxxxxx
```

当前内置厂商：`tavily`（原生即标准协议，复用 standardStrategy）、`bochaai`、
`searxng`。端点可经 `endpoint` 覆盖；新增厂商见「扩展方式」。

### 配置加载

`LoadConfig` 从账号池 YAML 的 `websearch` 段加载并合并默认值（max_results=5、
include_answer=true、search_depth=advanced）；文件缺失或解析失败返回默认值。
策略密钥同时接受 `api_key` 与 `apikey` 两种写法；`include_answer` 仅在显式
配置时覆盖。

## 数据流或生命周期

```text
账号池 YAML ──LoadConfig──▶ WebSearchConfig ──Assemble──▶ Strategy
                                                            ├─ standardStrategy（标准协议/自建网关）
                                                            ├─ tavily（复用标准协议）
                                                            ├─ bochaai（博查专有协议）
                                                            └─ searxng（SearXNG JSON API）
                                  Strategy.Search(ctx, query, maxResults)   ← 统一入参
                                                            ▼
                                              SearchResponse（统一出参模型）
                                                            │ FormatResponse
                                                            ▼
                                                  Markdown 返回给模型
```

装配发生在启动期（`tools/websearch.Register`）；每个搜索请求独立执行：
策略内 `http.Client` 每次 `Search` 只发一次请求，继承 caller context；
无共享可变状态，策略对象可在多次调用间复用。

## 依赖方向

- 允许依赖：标准库 `net/http` / `encoding/json` / `net/url`、`gopkg.in/yaml.v3`；
- 禁止反向依赖：不依赖 `seelebridge` 根包、工具注册层、GUI、`application` 或 Engine；
- 工具层（`tools/websearch`）只能通过 `Strategy` 接口消费，不复制业务状态机。

## 并发、存储、安全或错误语义

- 并发：策略无共享可变状态，可安全并发调用；
- 存储：本包不持久化任何数据；
- 安全：API key 只存在于配置与请求头中，绝不写入日志、Snapshot 或 README；
  示例只引用 `config/accounts.example.yaml` 的占位符风格；
- 错误语义：装配期错误说明「未配置策略 / 未找到策略 / 不支持 type / 缺少
  endpoint / 缺少 key / 未知 provider」，并列出可用策略或内置厂商；请求期错误
  带 `web_search:` 前缀；非 2xx 附带响应体片段便于排障；博查业务错误
  （HTTP 200 + code ≠ 200）单独拦截；context 取消能中断请求；
- 结果数上限：`limitResults` 把请求结果数限制在 1..`DefaultMaxResults`（10），
  超限回退配置默认值，避免异常大响应；SearXNG 返回固定条数，适配器按上限截断。

## 扩展方式

接入新搜索源有三种路径，按成本递增：

1. **标准协议端点 / 自建网关**（零代码）：`strategies` 声明
   `name / endpoint / 密钥` 即可，端点需兼容标准 websearch 协议（Tavily
   兼容：POST JSON + Bearer，响应含 `results[]`）；
2. **内置厂商适配器**（一个文件 + 注册）：厂商专有协议无法用网关收敛时，
   新增 `builtin_xxx.go`：实现 `Strategy`（专有请求构造 + 响应归一化），在
   `init()` 调 `registerBuiltin("厂商名", 工厂)`，装配逻辑零改动；
3. **其它非标准协议**：自建 Tavily 兼容网关（最省），或把构建器提升为公开
   装配 API。

新增内置厂商 checklist：

1. 新增 `builtin_xxx.go`，实现 `Strategy` 接口（出参必须归一为 `SearchResponse`）；
2. `init()` 注册 `registerBuiltin("name", factory)`；命名与厂商官方域名一致
   （如 bochaai）；
3. 在 README「内置厂商适配器」与账号池示例同步；
4. 补充装配与请求测试（httptest 按厂商真实响应样例）。

## Review 指南

- 是否仍存在「默认引擎」的隐性耦合？内置厂商只按 type / provider 显式装配，
  不预设默认；新厂商应走注册表而不是在 `Assemble` 加 if 分支；
- 新增适配器是否把厂商专有响应字段全部归一为 `SearchResponse`？入参/出参
  是否与标准策略一致（不泄漏厂商结构）？
- `legacyStrategy` 的旧字段翻译是否会误吞新配置（如 strategies 与 provider
  并存时）？provider 名是否只作 legacy 兜底与 active 选择？
- 策略配置错误（不支持 type、缺 endpoint、缺 key）是否在装配期提前拦截？
- `LoadConfig` 的默认合并是否会被零值误覆盖（尤其 include_answer）？
- API key 是否可能进入日志或返回给模型？
- 超时：`websearch.timeout` 与全局 `search_timeout`（旧名 `tavily_timeout`）
  的优先级是否清晰？

## 测试与验证

```text
go test ./seelebridge/search/... -count=1
go test ./seelebridge/tools/websearch/... -count=1
go test ./seelexctx/... -count=1
```

关键测试文件：

- `strategy_test.go`：无策略报错、旧字段翻译、最简策略装配、active 选择、缺 endpoint；
- `strategy_builtin_test.go`：type 分发、博查/SearXNG 端到端归一化、业务错误、
  旧 provider 翻译、未知 type/provider 报错；
- `config_test.go`：默认值、部分/全量覆盖、include_answer 显式覆盖、
  strategies 与 apikey 别名、type 解析；
- `strategy_standard_test.go`：httptest 验证请求体/鉴权/归一化、结果数上限、
  API 错误、context 取消、格式化。

## 文件与函数索引

### strategy.go

```go
type SearchItem struct { Title, URL, Content string; Score float64 }
type SearchResponse struct { Answer string; Items []SearchItem }
type Strategy interface { Name() string; Search(ctx context.Context, query string, maxResults int) (SearchResponse, error) }
const DefaultMaxResults = 10
func Assemble(cfg WebSearchConfig) (Strategy, error)                 // 装配器：选择并构建代理策略
func legacyStrategy(cfg WebSearchConfig) (StrategyConfig, bool)      // 旧字段翻译（tavily / 内置厂商名）
func strategyNames(strategies []StrategyConfig) string               // 返回可用策略名列表
func limitResults(defaultMax, requested int) int                     // 归一化请求结果数
func WebSearch(ctx context.Context, cfg WebSearchConfig, query string, maxResults int) (string, error) // 保留的兼容入口
```

### config.go

```go
type WebSearchConfig struct { /* provider/api_key/endpoint/max_results/include_answer/search_depth/timeout/active/strategies */ }
type StrategyConfig struct { Name, Type, Endpoint, APIKey, APIKeyAlias string }  // Type 空/standard=标准协议，否则内置厂商名
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

### builtin.go / builtin_tavily.go / builtin_bocha.go / builtin_searxng.go

```go
type BuiltinFactory func(cfg StrategyConfig, opts WebSearchConfig) (Strategy, error) // 厂商工厂：专有协议 → 统一 Strategy
var builtinVendors map[string]BuiltinFactory                                       // 内置厂商注册表（type/provider 名 → 工厂）
func registerBuiltin(vendor string, factory BuiltinFactory)                        // 注册厂商（各适配器 init 调用）
func isBuiltinVendor(name string) bool                                             // provider 兼容判定
func builtinNames() []string                                                       // 可用厂商名（字典序，报错提示用）
func assembleStrategy(cfg StrategyConfig, opts WebSearchConfig) (Strategy, error)  // 按 strategies[].type 分发
const defaultBochaEndpoint = "https://api.bochaai.com/v1/web-search"               // 博查默认端点
type bochaStrategy struct{ name, endpoint, apiKey string; opts WebSearchConfig; client *http.Client }
type bochaResponse struct{ Code, Msg, Data{Summary, WebPages{Value[]}} }           // 博查最小契约
type searxngStrategy struct{ name, endpoint string; opts WebSearchConfig; client *http.Client }
type searxngResponse struct{ Answers []string; Results []struct{...} }             // SearXNG 最小契约
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
strategy_builtin_test.go: TestAssemble_BuiltinTavilyByType / TestBochaStrategy_Search /
                         TestBochaStrategy_IncludeAnswerFalseOmitsSummary / TestBochaStrategy_DefaultEndpoint /
                         TestBochaStrategy_BusinessCodeError / TestSearxngStrategy_Search /
                         TestSearxngStrategy_MissingEndpoint / TestAssemble_UnknownBuiltinType /
                         TestAssemble_LegacyProviderBocha / TestAssemble_UnknownProviderListsBuiltins
config_test.go:          TestLoadConfig_Defaults / TestLoadConfig_InvalidYAML /
                         TestLoadConfig_PartialOverride / TestLoadConfig_FullOverride /
                         TestLoadConfig_EmptyAPIKeyKeepsDefault / TestLoadConfig_ZeroMaxResultsKeepsDefault /
                         TestLoadConfig_Strategies / TestLoadConfig_StrategiesApikeyAlias /
                         TestLoadConfig_StrategiesType
strategy_standard_test.go: TestStandardStrategy_Search / TestStandardStrategy_MaxResultsBounds /
                         TestStandardStrategy_APIError / TestStandardStrategy_ContextCancelled /
                         TestFormatResponse_WithAnswer / TestFormatResponse_NoAnswer /
                         TestFormatResponse_EmptyResults / TestFormatResponse_ResultsWithoutContent
```
