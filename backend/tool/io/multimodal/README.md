# Multimodal（多模态 wire 适配）

## 生态位

把会话媒体分区的 `media:<sha256>` 资产编码成 provider 可读的多模态内容，并提供一个
最小的带图同步请求客户端。主要调用方是 Seelex 侧需要“发图给模型”的能力（例如截屏
工具、真机冒烟测试），以及后续接入引擎对话链路的适配代码。

## 数据流图

```mermaid
flowchart LR
    MEDIA["store.MediaStore<br/>内容寻址媒体资产"] --> SRC["ImageSource<br/>读成可下发字节"]
    SRC --> ENC["编码 OpenAI 兼容 content parts<br/>text + image_url（data URL）"]
    ENC --> REQ["带图请求"]
    REQ --> CLIENT["最小带图同步客户端"]
    CLIENT --> PROVIDER["provider endpoint"]
    PROVIDER -->|失败| FAIL["显式失败语义（不静默降级成纯文本）"]
    NOTE["不做：截图 / 落盘 / 配额与 GC<br/>（归 store 媒体分区）"] -.-> MEDIA
```

## 职责与非职责

职责：

- `store.MediaStore` → `ImageSource`：把已落盘的媒体资产读成可下发字节。
- 编码 OpenAI 兼容 content parts：`text` + `image_url`（data URL）。
- 提供最小带图请求客户端与显式失败语义。

刻意不做：

- 不截图、不落盘、不管配额与 GC（那是 `store` 媒体分区的事）。
- 不替换引擎的对话循环。Seele 的 `types.Message.Content` 是 `*string`（v0.3.1 实测）
  （纯文本），带图请求无法经引擎下发；本包是这一能力缺口的 Seelex 侧适配，
  等 Seele 支持 content parts 后由引擎接管，本包只保留编码与冒烟用途。
- 不读取账号配置、不打印凭据（`Config.APIKey` 只进 Authorization 头）。

## 文件结构

| 文件 | 职责 |
|---|---|
| `content.go` | `ImageSource`/`Part`/`Message` 类型、`BuildParts`/`BuildMessage`/`ImageDataURL`/`LoadImage` |
| `client.go` | `Config`/`Answer`/`CompleteWithImages` 与 provider 响应解析 |
| `content_test.go` | 编码形状、base_url 归一化、假服务端下的请求/失败语义 |
| `live_smoke_test.go` | 真机冒烟（opt-in）：落盘 → 读回 → 带图请求 → 断言识别左红右蓝 |

## 核心实现

- `BuildParts(text, images)`：文本在前、图片随后；图片缺 MIME/字节直接报错
  （`ErrImageInvalid`）；带 `media:` 前缀的引用会校验 ref 合法性。
- `CompleteWithImages(ctx, Config, prompt, images)`：POST `<base_url>/chat/completions`，
  base_url 兼容带或不带 `/v1`、带或不带尾斜杠、已含路径三种写法。
- `Answer`：`Text`（`message.content`）、`Reasoning`（`reasoning_content`）、
  `FinishReason`、usage 与请求体字节数；`Answer.FinalText()` 在 content 为空时回退到
  reasoning——思考模式模型会把内容放在 reasoning_content，只读 content 会把
  「模型答了」误判成「模型没看到图」。
- wire 层图片上限 `DefaultMaxImageBytes`（5 MB，`ErrImageTooLarge`）：刻意低于会话
  媒体分区的落盘上限（8 MB），落盘上限管磁盘配额，wire 上限管 provider 收不收得下
  （data URL 还要再涨 33%）。

## 数据流

```text
会话媒体分区 meta/<hash>/<原名>
  └─ MediaStore.ReadMedia(ref) → ImageSource{mime, data}
       └─ BuildParts → [{type:text},{type:image_url,image_url:{url:data:...}}]
            └─ POST /chat/completions → Answer{Text/Reasoning/Usage}
```

## 依赖方向

- 允许依赖：`store`（读媒体资产）、标准库 `net/http`。
- 禁止反向：`store` 不依赖本包；本包不反向依赖 `application/`、`session/`。
- 真机冒烟测试额外依赖 `seelebridge/internal/config` 与 `internal/model` 解析账号，
  仅测试期使用。

## 错误语义

- 缺 model / 缺 api key / 缺 base URL：参数错误，显式返回。
- 非 2xx：返回状态码与截断后的响应正文（≤400 字符），便于区分「协议不认 content
  parts」与「鉴权/限流」。
- 200 但无 `choices`：显式报错，绝不把空回答当成“模型没看到图”。

## 扩展方式

新增 provider 协议（例如 Responses API 的 `input_image`）时，在本包内新增编码函数并
复用 `ImageSource`/`LoadImage`；不要改动 `store` 的媒体布局。

## Review 指南

- 图片字节是否原样透传（未经二次压缩/截断）。data URL 必须从原始字节重新编码。
- 失败路径是否显式（空回答、缺 choices、超限）——多模态链路最容易在这里误判。
- 是否把 APIKey 写进了日志或错误串。
- 大图是否在请求前按 wire 上限拒绝，而不是等 provider 返回 400。

## 测试与验证

```text
go test ./backend/tool/io/multimodal/ -count=1

# 真机冒烟（opt-in；只把路径交给加载器，不读取打印 accounts.yaml 内容）
$env:SEELEX_LIVE_SMOKE='1'
$env:SEELEX_ACCOUNTS_PATH='<path-to-accounts.yaml>'
go test ./backend/tool/io/multimodal/ -run TestLiveMultimodalImageSmoke -count=1 -v -timeout 300s
```

真机冒烟的断言刻意选择「文字提示里没有答案」的颜色词：模型答出 `red`/`blue` 只能来自
真正送达的图片字节；链路任一段静默丢图都会失败。已观测通过（2026-09-13，
`deepseek-v4-flash`：`finish=stop`、`content="red blue"`、`prompt_tokens=249`）。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### client.go

- `func (answer Answer) FinalText() string` — FinalText 返回可用于断言的完整回答：部分 provider（如 DeepSeek 思考模式）
- `func (config Config) endpoint() (string, error)` — endpoint 归一化 chat/completions 地址（兼容带或不带 /v1 的 base_url）。
- `func CompleteWithImages(ctx context.Context, config Config, prompt string, images []ImageSource) (Answer, error)` — CompleteWithImages 发起一次带图同步请求并返回模型回答。
- `func errorSnippet(body []byte) string` — errorSnippet 截断错误正文用于诊断；响应正文可能很长，但绝不含请求凭据。

### content.go

- `func LoadImage(ctx context.Context, st MediaReader, key dto.SessionKey, ref string) (ImageSource, error)`
- `func ImageDataURL(mimeType string, data []byte) string` — ImageDataURL 编码 data URL：`data:<mime>;base64,<payload>`。
- `func BuildParts(text string, images []ImageSource) ([]Part, error)` — BuildParts 把提示文本与图片编码成 content parts；图片顺序即展示顺序。
- `func BuildMessage(text string, images []ImageSource) (Message, error)` — BuildMessage 装配一条带图 user 消息。
- `func RequestBody(model string, message Message, maxTokens int, temperature float64) map[string]any` — RequestBody 返回 wire 请求体所需的字段（供客户端与测试复用同一编码）。

### content_test.go

- `func testImage() ImageSource`
- `func TestBuildPartsShapes(t *testing.T)` — TestBuildPartsShapes 锁定 wire 形状：文本在前、图片为 image_url data URL。
- `func TestAnswerFinalTextFallsBackToReasoning(t *testing.T)` — TestAnswerFinalTextFallsBackToReasoning 锁定真机踩到的差异：思考模式下
- `func TestConfigEndpoint(t *testing.T)` — TestConfigEndpoint 覆盖 base_url 归一化（带 /v1、带尾斜杠、已含路径）。
- `func TestCompleteWithImagesPostsParts(t *testing.T)` — TestCompleteWithImagesPostsParts 用假服务端校验请求体、鉴权头与响应解析。
- `func TestCompleteWithImagesFailsExplicitly(t *testing.T)` — TestCompleteWithImagesFailsExplicitly 覆盖三类失败必须显式报错，不得返回空回答。

### live_smoke_test.go

- `func (reader routerMediaReader) ReadMedia(ctx context.Context, key dto.SessionKey, ref string) (store.MediaRef, []byte, error)`
- `func TestLiveMultimodalImageSmoke(t *testing.T)` — TestLiveMultimodalImageSmoke 是真机多模态冒烟（opt-in）：
- `func findStoredFile(t *testing.T, root, fileName string) (string, string)` — findStoredFile 在数据根里定位落盘文件，返回绝对路径与其父目录名（应为内容 hash）。
- `func liveSplitPNG(t *testing.T, width, height int) []byte` — liveSplitPNG 生成左红右蓝的 PNG（纯色块，任何视觉模型都能判色）。
