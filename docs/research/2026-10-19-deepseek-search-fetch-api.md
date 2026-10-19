# DeepSeek 官方 Search & Fetch API（2026-10-17 上线）

> 调研时间 2026-10-19，来源为 DeepSeek 官方 API 文档（`api-docs.deepseek.com`）在线页面，
> 逐条核对后落成。协议事实来自文档；接入侧既有 `httptest` 用例钉住形状，
> 也做过**一次真实调用**（用账号池里已有的 DeepSeek key，不落地、不回显密钥）：
> 策略装配为 `deepseek`，返回 3 条结果，`rerank_score` 2.43 / 2.39 / 2.34，
> 中英韩三语命中，`summary` 长度 2.2K~3.3K 字。

## 1. 结论（先看这段）

- DeepSeek 在 **2026-10-17** 上线了自己的 **Search & Fetch API**，由**自建搜索引擎**提供
  （官方口径：日均处理数亿次请求），是"厂商自己的搜索"，与博查（BochaAI）/Tavily 同级。
- 形态是**裸 JSON API**，与模型 API 共用同一把 Bearer key、同一个 base URL
  `https://api.deepseek.com`。
- 计费按**成功请求**计（空结果、失败、超时不收费）：Search 与 Fetch 各 **$0.75 / 千次**；
  在 DeepSeek Harness / Claude Code / Codex 里走服务端工具调用，**免到 2026-11-01 00:00（北京）**。
- 对本仓库的意义：`backend/tool/search` 的"内置厂商适配器"正是为这种场景准备的，
  新增一家 = 一个文件 + 一个 `init()` 注册 + 配置里写 `type: deepseek`。
  **已经接入**（`builtin_deepseek.go`，见 §4）。

## 2. 协议事实（官方文档逐条）

### 2.1 端点与鉴权

| 项 | 值 |
|---|---|
| Search | `POST https://api.deepseek.com/v1/tools/search` |
| Fetch | `POST https://api.deepseek.com/v1/tools/fetch` |
| 鉴权 | `Authorization: Bearer <DeepSeek API Key>`（与模型 API 同一把） |
| Content-Type | `application/json` |

### 2.2 Search 请求

| 字段 | 类型 | 必填 | 默认 | 说明 |
|---|---|---|---|---|
| `query` | string | 是 | — | 1..128 个 Unicode 字符，不能全空白；**支持算子**（见 2.4） |
| `count` | int | 否 | 10 | 1..50；只影响每页条数，**不影响按次计费** |
| `ori_question` | string/null | 否 | — | 触发这次搜索的原始问题（≤128 字符），用于改善查询理解 |
| `localization` | string | 否 | `""` | 可选 IP，用于本地化结果 |
| `need_content` | bool | 否 | false | 为 false 时响应里的 `content` 恒为空串 |

未知字段被忽略。

### 2.3 Search 响应

`{ "results": [ ... ] }`，未命中返回空数组。

| 字段 | 类型 | 说明 |
|---|---|---|
| `rerank_score` | number | 0..3 的相关性分（综合相关/权威/新鲜度），**≥1.5 视为高质量** |
| `title` | string | 标题 |
| `url` | string | 链接 |
| `snippet` | string | 100~200 字，面向 UI 展示的短摘要 |
| `summary` | string | 600~800 字，**面向模型阅读**的长摘要 |
| `content` | string | 页面正文；`need_content=false` 或正文不可得时为空 |
| `site_name` / `site_icon` | string/null | 站点名与图标 |
| `date` | RFC3339/null | 发布时间（UTC） |
| `authority` | int | -1..4 的权威层级；**≥3 为高权威来源** |

### 2.4 查询算子（query 原样转发）

| 算子 | 例 | 作用 |
|---|---|---|
| `site:` | `DeepSeek site:github.com OR site:huggingface.co` | 限定站点（多站点用空格 + OR） |
| `-site:` | `DeepSeek -site:example.com` | 排除站点 |
| `-term` / `-"phrase"` | `DeepSeek -advertisement` | 排除词/短语 |
| `"..."` | `DeepSeek "exact phrase"` | 精确短语 |
| `lang:` | `lang:zh,en` | 语言限定 |
| `after:` / `before:` | `after:2025-01-01` | 发布时间区间 |

### 2.5 Fetch

- 请求 `{"url": "..."}`：≤8192 字符，必须是 http/https 且有 host，**不能带用户名/密码**。
- 响应 `{"title": "...", "content": "..."}`，`content` 是 Markdown。
- 受目标站可用性与访问策略约束。

### 2.6 错误语义

OpenAI 兼容错误格式：`{"error":{"message","type","param":null,"code"}}`。

| HTTP | 含义 | 可重试 |
|---|---|---|
| 400 | 请求头/体非法（含非法字段值） | 否（改请求再试） |
| 401 | key 缺失或无效 | 否 |
| 402 | 余额不足 | 否（充值后） |
| 403 | Fetch 被安全策略拒绝 | 否 |
| 404 | Fetch 取不到可用正文 | 否 |
| 408 / 429 / 5xx | 读体超时 / QPS 超限 / 服务端错误 | **是**（等待重试） |
| 413 | 请求体过大 | 否 |
| 504 | 下游超时 | 是 |

### 2.7 计费与限流

- $0.75 / 千次**成功请求**（Search、Fetch 各自独立计价）；只有 HTTP 200 且产出结果才计费。
- QPS（账号级，与模型并发无关）：**Search 100**、**Fetch 25**；Fetch 另有按域名的 QPS 限制。
- 超限一律 429。

## 3. 与本仓库现状的对照

| 维度 | 当前配置（博查 BochaAI） | DeepSeek Search |
|---|---|---|
| 端点 | `https://api.bochaai.com/v1/web-search` | `https://api.deepseek.com/v1/tools/search` |
| 鉴权 | 独立 key | **与模型同一把 key**（省一个账号） |
| 响应形态 | `{code,msg,data.webPages.value[]}` | `{results[]}`（OpenAI 风格错误体） |
| 每条结果 | `name/url/snippet` + 可选整体 `summary` | `title/url/snippet/summary/content` + `rerank_score/authority/date` |
| 计费 | 按次（厂商口径） | 按成功次，$0.75/千次；首两周服务端工具调用免费 |
| 算子 | 有限 | site/ -site/ -term/ "phrase"/ lang:/ after:/ before: |

差异要点：DeepSeek **没有"跨结果综合答案"**（博查的 `data.summary` 是整体摘要），
它给的是一条条结果的 `summary`；`authority` / `rerank_score` 是本仓库目前**没有展示**的
信号（`SearchItem.Score` 已经能带 `rerank_score`）。

## 4. 接入落点（已实现，未提交）

- `backend/tool/search/builtin_deepseek.go`：内置适配器，`init()` 注册厂商名 `deepseek`。
  - `count = clamp(limitResults(opts.MaxResults, maxResults), 1..50)`；
  - `need_content` 恒 `false`（正文归 Fetch；`summary` 已够模型读）；
  - `SearchItem.Content = summary → snippet`，`Score = rerank_score`；
  - `include_answer=true` 时 `Answer` **仍为空**（DeepSeek 没有跨结果综合答案，把 Top-1
    摘要顶上去会让同一段话印两遍），`max_results` 建议 5——一条 summary 就有 600~800 字；
  - 空结果 = 0 条 + nil error；非 200 = 错误，文案取 `error.message (code)`。
- `backend/tool/search/config.go`：`StrategyConfig` 新增 `api_key_env`（可选）——
  `api_key`/`apikey` 都空时读该环境变量，让 `search_engine.yaml` 能进版本库而不带密钥。
- `config/search_engine.example.yaml`：加 `type: deepseek` 示例与厂商名。
- `backend/tool/search/README.md`：厂商表、文件表、DeepSeek 专节（字段映射与取舍）。
- 测试 `builtin_deepseek_test.go`（6 例，httptest 复刻真实响应）：请求形状与鉴权、
  字段映射与兜底、空结果非错误、错误信封、缺 key、端点/`count` 钳制、
  `provider: deepseek` 简写装配。

**启用方式**（已落到本仓库的实际配置里）：

```yaml
# config/search_engine.yaml
provider: deepseek          # 官方 Search API
# 不写 api_key：默认从 accounts.yaml 里取同一把 DeepSeek key（见下）
```

密钥三级落点：`api_key`（显式）→ `api_key_env`（环境变量名）→ **账号池同厂 key**
（`wiring/config.DeepSeekSearchKey()`：按 `base_url` 域名或 `model` 前缀认出 DeepSeek 账号，
角色偏好 agent → subagent → goalplan → 其余字典序，取第一把非空 key）。
DeepSeek Search 与模型 API 共用同一把 key，所以第二份配置通常不必存在。

## 5. 刻意没做的（留作后续）

- **Fetch 工具**：DeepSeek 的 `/v1/tools/fetch`（URL → Markdown）没有接。它是一个**新工具**
  （不是 web_search 的另一种策略），要先定"谁能调、抓来的正文进不进上下文、怎么截断"；
  现在接等于偷偷给模型开一条新的抓取通道。
- **`need_content: true`**：会把每条结果的正文一起拉回（每次几十 KB）；要正文应当走 Fetch，
  而不是把 Search 当正文通道。
- **`ori_question` / `localization`**：`Strategy.Search(ctx, query, maxResults)` 的签名里
  没有这两格；要用得先扩接口（跨所有厂商），本次不动。
- **自动重试**：408/429/5xx 官方允许重试，但现有所有厂商适配器都是"一次性请求 + 明确报错"；
  加重试是**全局策略**，应当一处实现（工具层），不在单厂商里开特例。

## 6. 证据与来源

- 官方文档（2026-10-19 取）：
  - `https://api-docs.deepseek.com/guides/search`（Search & Fetch 指南：算子、错误、计费、限流）
  - `https://api-docs.deepseek.com/api/search`（Search 请求/响应 Schema）
  - `https://api-docs.deepseek.com/api/fetch`（Fetch Schema）
  - `https://api-docs.deepseek.com/news/news261017`（上线公告）
- 本地验证命令：`go vet ./backend/tool/search/`、`go test ./backend/tool/search/ -count=1`
  （含 `-run DeepSeek` 六例）。
- **未验证**：Fetch API 的接入（本次只接 Search）；`need_content: true` 的载荷规模。
