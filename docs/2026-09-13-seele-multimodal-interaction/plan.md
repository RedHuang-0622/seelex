# Seele 多模态改造方案：图片上传交互 + 限流与并发

> 状态：**规划（未实现）**。本文是 seele/seelex 两侧的改造计划与接口契约草案，
> 不声称任何能力已经可用。已实现部分见 `sessionstore/README.md`（会话媒体分区）与
> `seelebridge/multimodal/README.md`（wire 编码 + 真机冒烟）。
>
> 日期：2026-09-13 ｜ 范围：Seele v0.1.3（上游依赖）+ seelex 输入与装配层

## 0. 结论摘要

1. **上游已有对口缺口**：Seele 自己的路线图把多模态列为 G13
   （`docs/plan/07-v05-roadmap-gap-analysis.md:63,337`：「image/audio/file attachment
   → Content 结构扩展」，估计 1-2 天）。本方案就是把 G13 落成可合并的接口契约。
2. **改造的正确切点是「引用而非字节」**：`Message` 里只放媒体**引用**（见 §3.1），
   字节留在 seelex 的会话媒体分区（上一轮已实现）。否则 `seelectx/cache` 的
   `MaxEntrySize` 默认 1 MB（`seelectx/cache/config.go:38`，超限拒写
   `seelectx/cache/filecache.go:139`）会让带图历史**写不进缓存**，压缩提示词
   （`seelectx/ctx_manager/history.go:205,213`）也会把图片当文本丢进摘要。
3. **交互不新增前端专用通道**：图片上传走「媒体分区 + 引用」，命令面用
   `/attach`（注册点 `application/core/command.go:11-19`，四个前端同时生效），
   GUI 补一个文件选择/拖放，TUI 靠「拖进终端的路径嗅探 + `/attach`」。
4. **限流现状是真空**：Seele 的 `agent/core/api` **没有任何重试**，`accountpool`
   **没有速率限制**（只有并发信号量 `accountpool/pool.go:21-28,168-202`），带
   退避的熔断器只存在于 MCP 工具（`tools/mcp/breaker.go:44-55,122-126`）。所以
   「并发」可直接复用租约，「限流」（RPM/TPM）与 429 退避必须新建。
5. **超额度的默认答案是「可见拒绝，而不是静默丢图」**：一次性最多 N 张（默认 8），
   超出按 §5.2 阶梯处理——先降采样、再按显式策略裁剪并插入**用户可见**的降级说明，
   最后才是拒绝；任何一步都不允许悄悄少发一张。

---

## 1. 目标与非目标

### 目标

- 用户能在 CLI/TUI/GUI 三个前端**上传图片**（本机文件、拖放、GUI 选择框），
  并让模型真正看到图片（不是路径字符串）。
- 图片作为**会话资产**可引用、可复用、可回收，重启不丢、重试不重复上传。
- 有明确且可配置的**限额**：单次张数、单张体积、单条总量、像素长边、会话磁盘配额。
- 有明确的**限流与并发**语义：并发租约、请求速率（RPM/TPM）、429 退避、队列背压。
- 超额度的行为**可预测、可解释、用户可见**。

### 非目标

- 不做音频/视频/PDF 解析（G13 里同类，但成本与合规不同，单独评估）。
- 不做图片生成/编辑回写。
- 不改变工具调用协议（tool 结果的图片继续走 `ToolResult.Multimodal`，与用户附件
  共用媒体分区，但**不共用**配额策略：工具结果由 Agent 产生、用户附件由人产生）。
- 不在 Seele 里实现工作区路径门禁（那是 seelex 的 `ProjectScope` 职责）。

---

## 2. 事实基线（已核实，file:line）

### 2.1 阻塞点

| # | 位置（Seele v0.1.3 模块内相对路径） | 事实 | 影响 |
|---|---|---|---|
| B1 | `types/model.go:57-65` | `Message.Content *string`，且**没有自定义 `MarshalJSON`** | 直接决定 OpenAI 请求体字节；新增字段会自动出现在请求里 |
| B2 | `agent/core/api/strategy_openai.go:16,25,42` | 请求/响应结构体**直接复用 `[]types.Message`** | 想发 content parts 必须改类型或加自定义序列化 |
| B3 | `agent/core/api/strategy_anthropic.go:277-347` | 手写 content block，只认 `*m.Content`，`json.Marshal(*m.Content)` 当字符串塞进去 | Anthropic 分支必须单独适配 |
| B4 | `session/loop.go:18,165,224,537` | `Run(ctx, userInput string, ...)`；`history = append(..., types.Message{Content:&userInput})`；持久化 `json.Marshal(history)` | 引擎入口签名与历史落盘都在字符串上 |
| B5 | `types/model.go:11-16,23-40` | `ChatCompleter` 以 `[]Message` 进出、`onChunk(delta string)`；`StreamEventType` 6 态、`StreamEvent.Content string` | 流式事件没有图片位置（本方案不改口，见 §3.5） |
| B6 | `seelectx/cache/config.go:38`、`filecache.go:139,156` | 默认 `MaxEntrySize = 1MB`，超限**拒写**；目录名 = value 的 SHA256 | 结论 2 的直接依据：字节不能进 Message |
| B7 | `seelectx/ctx_manager/history.go:205,213,221,167` | 压缩输入按 `"User: "+*m.Content` 拼纯文本 | 压缩会把图片静默丢成文本 |

### 2.2 seelex 侧入口与可复用件

| # | 位置 | 事实 |
|---|---|---|
| S1 | `application/core/service_input.go:64-79` | 公开入口是 `Submit(ctx, text string)`——**全链路单字符串** |
| S2 | `application/core/input_router/router.go:26-31,44-50` | 路由顺序 `／`→命令、`#`→skill、`@`→plugin、兜底对话（`@` 已被 plugin 占用） |
| S3 | `application/core/command.go:11-19` | `register(name, description, execute)`：新命令一处注册，**四个前端同时生效** |
| S4 | `application/contract/ports.go:17-24`、`internal/adapters/messages.go:15-32` | `EngineMessage{Content string; ContentSet bool}` 是引擎边界；`adaptMessages/restoreMessages` 双向转换 |
| S5 | `application/model/state.go:101-112` | 展示层 `Message.Content string`——前端只能渲染字符串 |
| S6 | `session/ports.go:59-81` | `InputQueue.Enqueue(text string, payload any)` **无容量上限** |
| S7 | `workspace/readfile.go:33-77` | 已有「工作区内读文件 → base64 + 默认 4 MiB / 硬限 64 MiB + `truncated` 标记」的现成通道 |
| S8 | `gui/dialogs_gui.go:9-11`、`gui/frontend/dist/app.js:2016-2021` | GUI 已有原生**目录**选择框；composer 提交是 `invoke("Submit", text)` |
| S9 | `seelebridge/security/project_scope.go:124-130,134-152` | 无回退根、拒绝绝对路径与符号链接逃逸；`seelebridge/security/pathgate.go` **无调用点（死代码）** |
| S10 | `seelexctx/tokens/tokens.go:23,41,57` | token 估算 = `cjk + (ascii+3)/4 + (other+1)/2`，**完全没有图片项** |
| S11 | `sessionstore/media.go`（已实现） | `meta/<sha256>/<原名>`、`media:<hash>` 引用、`MediaStore` 能力接口、配额与 GC |
| S12 | `seelebridge/multimodal/`（已实现） | content parts 编码 + 最小带图客户端；真机冒烟已通过 |

### 2.3 限额与并发基线

- **已有**：账号级并发租约（`accountpool/pool.go:21-28` 每账号 `chan struct{}` 容量 =
  `MaxConcurrency`；`pool.go:168-202` 饱和等待不占容量、`ctx` 取消即退出）；
  P2C 选择器按 `Active/MaxConcurrency` 打负载分（`accountpool/selector.go:21-29,36-85`）；
  seelex 侧按角色/分支稳定路由（`seelebridge/account/account.go:30-38,49-55,106-124`，
  `seelebridge/account/manager.go:155-168,180-203`）；MCP 熔断退避
  （`tools/mcp/breaker.go:44-55,93,122-126`，base 5s、max 60s）。
- **缺口**：Seele `agent/core/api` 无任何重试；`accountpool` 无 RPM/TPM 令牌桶；
  请求里的图片数与成本**不参与**并发计量（8 张图和 1 张图占同一档租约）；
  入队队列无容量上限（S6）；token 估算无图片项（S10）。

---

## 3. 方案 A：Seele 侧「引用式 content parts」最小侵入改造

### 3.1 数据模型：Message 只携带引用

```go
// types/model.go 新增（Message 保持字段只增不改）

// ContentPart 是消息的一段内容。刻意用「引用」而不是内联字节：
// 历史 JSON、filecache、context 压缩、token 估算都不该看到 base64。
type ContentPart struct {
    Type     string        `json:"type"`                // "text" | "image"
    Text     string        `json:"text,omitempty"`
    Image    *ImageRef     `json:"image,omitempty"`
}

type ImageRef struct {
    Ref      string `json:"ref"`                 // 例如 "media:<sha256>"
    MimeType string `json:"mime_type"`
    Name     string `json:"name,omitempty"`      // 原名，仅供诊断
    Bytes    int    `json:"bytes,omitempty"`
    Width    int    `json:"width,omitempty"`
    Height   int    `json:"height,omitempty"`
    Detail   string `json:"detail,omitempty"`    // "auto" | "low"，成本杠杆
}

type Message struct {
    Role             string        `json:"role"`
    ReasoningContent string        `json:"reasoning_content,omitempty"`
    Content          *string       `json:"content,omitempty"`   // 兼容：纯文本消息照旧
    Parts            []ContentPart `json:"parts,omitempty"`     // 新增：多模态
    ToolCalls        []ToolCall    `json:"tool_calls,omitempty"`
    ToolCallID       string        `json:"tool_call_id,omitempty"`
    Name             string        `json:"name,omitempty"`
    Usage            *Usage        `json:"-"`
}
```

不变式：

- `Content == nil && len(Parts) == 0` → 无正文（现有语义）；
- 只有 `Content` → **wire 输出字符串**，字节与今天完全一致（B1 的无自定义序列化
  保证了这一点，改造后靠自定义序列化继续保持）；
- 有 `Parts` → wire 输出 parts 数组，其中 `text` part 可由 `Content` 自动补齐，
  避免调用方同时维护两个字段。

### 3.2 JSON 兼容：只在「有 parts」时才改变形状

```go
// types/model.go：新增自定义编码，无 parts 时完全走旧路径
func (m Message) MarshalJSON() ([]byte, error) {
    if len(m.Parts) == 0 {
        type legacy Message // 去掉方法的别名，避免递归
        return json.Marshal(legacy(m))
    }
    // 有 parts：content 输出为数组（OpenAI 与 Anthropic 都接受数组形态）
    // text part 由 Content 补齐；image part 由调用方提供引用
}
```

要点：

- **wire 形状与「序列化后的历史」解耦**：历史落盘（`session/loop.go:537`）继续存
  `Parts` 里的**引用**，不在磁盘上出现 base64；真正展开成 data URL 只发生在
  「构建 HTTP 请求」那一步（§3.4）。
- `UnmarshalJSON` 需接受三种输入：字符串、数组、缺省，保证旧会话可读、新会话可回放。
- 兼容边界必须测试：旧 JSON 读入 + 写回**逐字节等价**（回归测试，见 §6 P0）。

### 3.3 provider 映射（三种协议）

| 协议 | 位置 | text part | image part |
|---|---|---|---|
| OpenAI chat/completions | `strategy_openai.go` | `{"type":"text","text":...}` | `{"type":"image_url","image_url":{"url":"data:<mime>;base64,...","detail":...}}` |
| Anthropic messages | `strategy_anthropic.go:277-347` | `{"type":"text","text":...}` | `{"type":"image","source":{"type":"base64","media_type":...,"data":...}}` |
| OpenAI Responses（可选 P2） | 新 strategy | `input_text` | `input_image`（本仓库调研文档已验证该形态可用） |

`strategy_openai.go` 的同步/流式请求结构体（`:16,25`）当前直接复用
`[]types.Message`，改造后**继续复用**——因为 parts 展开发生在 `Message`
自己的序列化里，请求结构体不用动，这是本方案侵入面最小的关键。

### 3.4 媒体解析钩子：字节只在请求构建期出现

```go
// types（或 api）包新增可选能力，默认 nil = 不支持图片
type MediaResolver interface {
    // Resolve 返回原文字节；ref 不存在时返回错误（绝不返回空字节假装成功）
    Resolve(ctx context.Context, ref string) (data []byte, mimeType string, err error)
}
```

- 注入点：`api.ChatClient` 增加 `SetMediaResolver(MediaResolver)`；`agent` 装配层
  透传。seelex 侧用 `sessionstore.MediaStore` 适配（`seelebridge/multimodal` 已
  具备同等能力，可直接作为参考实现）。
- **展开缓存**：`(ref, detail)` → data URL 字符串缓存（进程内 LRU，条数上限按
  §5 张数上限 × 历史窗口估算），避免每次重试重新 base64 编码 8 MB。
- **无 resolver 时的行为必须显式**：`Parts` 里有 image 但没有 resolver →
  返回错误 `ErrMediaResolverMissing`，不允许静默降级成「只有文本」——那正是
  本方案最想避免的失效模式。

### 3.5 流式、事件与工具

- `StreamEvent`（`types/model.go:23-40`）**不改**：图片只出现在输入侧，
  输出侧没有图片增量；保持 6 态可避免所有前端 reducer 改动。
- `ChatCompleter` 接口签名**不改**（`[]Message` 进出）：改动集中在数据类型内部。
- tool 调用参数是 JSON 字符串（`types/model.go:68-78`），工具返回图片继续
  走「工具结果引用」而非 message parts（seelex 已有 `ToolResult.Multimodal`）。

### 3.6 上下文与持久化的连带修复（必须同批做，否则带图会话会静默劣化）

| 位置 | 现状 | 需要的改动 |
|---|---|---|
| `seelectx/ctx_manager/history.go:205,213,221` | 按 `*m.Content` 拼文本 | parts 里的图片替换为**稳定占位符**（如 `[image:<ref 前缀>]`），使摘要仍能表达「这里有过图」且 token 可控 |
| `seelectx/cache/config.go:38` | 默认 1 MB 拒写 | 引用式 parts 体积很小，通常无需调；但需**显式测试**「带 10 张图的历史 < 1 MB」以防回归 |
| `seelexctx/tokens/tokens.go:23,41,57`（seelex 侧） | 无图片项 | 按 §5.3 的 tile 公式计入；否则 wire 预算会低估 |
| `session/loop.go:537` | `json.Marshal(history)` | 依赖 §3.2 的自定义序列化，无需改动；加回归测试锁定「历史 JSON 内不含 base64」 |
| redaction/日志 | 未发现任何脱敏（子代理确认） | 新增 parts 进日志前必须只打印 `ref/mime/bytes`，**绝不打印 base64** |

### 3.7 上游落地方式

- Seele 仓库 PR-1：`types` 数据模型 + 自定义序列化 + 单测（旧 JSON 等价、三种输入形态）。
- Seele 仓库 PR-2：`api` 的 `MediaResolver` + OpenAI/Anthropic 两个 strategy 的 parts 映射。
- Seele 仓库 PR-3：`ctx_manager` 占位符 + `loop` 回归测试（历史不含 base64）。
- seelex 侧 PR-4：`EngineMessage` 增 `Parts`（§4.5）+ 适配器双向转换。
- 若上游不接受自定义序列化：退路是新增 `MessageWire` DTO 在 `api` 层转换
  （只改 `strategy_*.go`），代价是 `strategy_openai.go:16,25` 的请求结构体要换类型。

---

## 4. 方案 B：上传图片的交互改造

### 4.1 统一附件模型与「先落盘、后入队」

```go
// application/contract/dto（纯数据）
type AttachmentRef struct {
    Ref      string `json:"ref"`        // media:<sha256>
    Name     string `json:"name"`
    MimeType string `json:"mime_type"`
    Bytes    int    `json:"bytes"`
    Width    int    `json:"width"`
    Height   int    `json:"height"`
    Scale    float64 `json:"scale,omitempty"`
}
```

三条硬规则：

1. **附件在入队前落盘**为 `media:<hash>`，队列与历史只带引用。理由：`InputQueue`
   无容量上限（S6），把字节放进 `Payload any` 等于把内存交给用户输入。
2. **引用一旦写入消息即固化**（写进工具结果/事件行的 `multimodal` 字段），
   重试同一轮不会重复上传，也不会因为源文件被删而失效。
3. **源文件永不被原地引用**：工作区内文件也必须拷贝进媒体分区。删除源文件不影响
   会话可回放。

### 4.2 命令面（一处注册，四端生效）

`/attach <path>...`：注册进 `application/core/command.go:11-19`。

- 只做「读文件 → 校验 → 落盘 → 挂到下一次提交的草稿附件列表」；
- 失败语义显式：路径不存在 / 不是图片 / 超字节上限 / 长边超限，各给单独文案；
- `/media`（P1）：列出本会话媒体（`ListMedia`）、显示占用、`/media rm <ref>` 删除，
  与 §5.2 的「配额打满」出口对应。
- 命令名避开 `@`（插件路由，S2）与 `#`（skill 路由）。

### 4.3 CLI/TUI

| 交互 | 实现 | 说明 |
|---|---|---|
| 拖文件进终端 | 终端会把路径**当文本插入**输入行 | 提交前做路径嗅探：整行/整段是「存在且扩展名属于图片集」的路径 → 转为附件并提示「已附加 1 张」；误判风险用「必须整段且文件存在」两道闸门压住 |
| 显式路径 | `/attach`；CLI 亦可 `seelex --image <path> "prompt"` | 适合脚本化与冒烟 |
| 粘贴 | 文本剪贴板可直接粘贴路径 | `github.com/atotto/clipboard` **只有文本**，不承诺图片剪贴板；图片粘贴列为 P2（需要平台专用实现：Windows CF_DIB / macOS png pasteboard） |

TUI 展示：附件以 chips 形式显示在 composer 上方（`文件名 1.2MB 2048×1536`），
提交后气泡里显示缩略占位（TUI 无图形时显示 `[图片: 原名]`），**绝不显示 base64**。

### 4.4 GUI

| 交互 | 实现 |
|---|---|
| 选择文件 | `gui/dialogs_gui.go` 增加 `PickImageFiles()`（`runtime.OpenFileDialog` + 图片 Filters），与已有 `PickDirectory`（`:9-11`）同构 |
| 拖放 | Wails 拖放事件 → 路径列表 |
| 提交 | 新增绑定 `SubmitWithAttachments(text string, refs []dto.AttachmentRef)`；**保留** `Submit(text)`（`gui/frontend/dist/app.js:2021`）以兼容 |
| 展示 | composer 附件 chips（可删）、消息内缩略图与像素/体积信息；缩略图由后端出（复用媒体分区读 + 降采样），避免前端拉原图 |
| 上传进度 | 大图落盘 + 降采样是 CPU/IO 活，走 §5.4 的有界工作池并回传进度事件；不做「假的同步等待」 |

### 4.5 契约与展示层改动

| 层 | 现状 | 改成 |
|---|---|---|
| `application/contract/ports.go:17-24` | `EngineMessage{Content string; ContentSet bool}` | 增 `Parts []EngineContentPart`（text/image 引用），适配器 `internal/adapters/messages.go:15-32` 双向映射到 Seele `Message.Parts`/`Content` |
| `application/model/state.go:101-112` | `Message.Content string` | 增 `Attachments []dto.AttachmentRef`（展示用，不参与 provider 历史） |
| 前端 reducer | 只渲染字符串 | 渲染附件 chips/缩略图；未知字段忽略即可（增量式，不破坏旧前端） |
| `application/core/skill_context.go:16-21` | `chatRequest{displayInput, modelInput string}` | 增 `attachments []dto.AttachmentRef`，并在模型输入里注入一行文本说明（见 §4.6） |

### 4.6 模型侧提示词约定

用户附件与工具截屏必须可区分，建议在用户消息文本前注入一行**机器可读**说明：

```text
[attachments: 2 | media:ab12… 1920x1080 image/png | media:cd34… 800x600 image/png]
```

理由：模型因此知道图片是用户主动提供的上下文，而不是工具产物；压缩时该行也作为
占位符保留（§3.6），不会让摘要丢掉「这里有过图」。

### 4.7 外部路径的合规入口

`ProjectScope` 无回退根、拒绝绝对路径逃逸（S9），`PathGate` 是**死代码**——
工作区外的图片当前没有合规读取路径。方案：

- 用户**显式 attach**（点选/拖放/命令行给出绝对路径）= 明确的用户同意，属于
  该门禁的合法例外；
- 实现上走单一入口 `AttachExternalImage(path)`：解析绝对路径 → 拒绝符号链接与
  目录 → 按图片魔数校验格式（不信任扩展名）→ 拷进媒体分区 → 返回 `AttachmentRef`；
- **不**沿用 `PathGate`（先清死代码或明确其定位，别让「看起来存在但没人调用」的
  门禁成为审计错觉）；
- Agent 工具**不得**获得该入口：`/attach` 是人类输入面，不是工具能力。

---

## 5. 方案 C：限额、并发与限流

### 5.1 限额四轴 + token 预算

| 配置键（新增于 `limits` 段） | 默认值 | 轴 | 超限错误 |
|---|---|---|---|
| `multimodal_max_images` | 8 | 单条消息张数 | `ErrTooManyImages` |
| `multimodal_wire_image_bytes` | 5 MiB | 单张（provider 侧） | `ErrImageTooLarge`（已实现于 wire 层） |
| `multimodal_max_item_bytes` | 8 MiB | 单张（落盘） | `ErrMediaTooLarge`（已实现） |
| `multimodal_max_message_bytes` | 24 MiB | 单条消息合计 | `ErrMessageMediaBudget` |
| `multimodal_max_long_side` | 4096 px | 像素 | `ErrMediaDimensions`（已实现） |
| `multimodal_session_quota_bytes` | 256 MiB | 会话磁盘 | `ErrMediaQuota`（已实现） |
| `multimodal_history_turns` | 3 轮 | 历史中保留图片的轮数 | 无错误：超窗替换为占位符（成本控制） |

默认值的取值理由：8 张 ≈ 一次「多图对比/几页截图」的真实需求上限；24 MiB ≈ 8×3 MiB
留出余量但挡住 8×5 MiB 的极端；4 轮窗口把每轮重复计费的图片 token 摊到可接受范围。

### 5.2 超限处理阶梯（本方案的核心答案）

**原则：静默丢图是最坏结果**——模型答错、用户不知道为什么。所以阶梯的每一级要么
成功，要么**可见降级**，要么**显式拒绝并指名**。

```text
上传 N 张
 ├─ N > multimodal_max_images ?
 │    ├─ 默认：拒绝整条，错误含「已选 N / 上限 M / 请去掉 K 张或分两轮」
 │    └─ 显式策略 overflow=oldest_first：保留最近 M 张，正文顶部插入可见说明
 │        「⚠️ 本消息有 K 张图因超过 M 张上限未上传：<文件名列表>」
 ├─ 张数 OK → 逐张处理
 │    ├─ 长边 > max_long_side → 自动降采样（clamp 长边 → 重新编码），最多 3 次
 │    ├─ 体积 > wire_image_bytes → 继续降（质量/比例），最多 3 次
 │    ├─ 仍超 → 该张拒绝并指名（其余照发，用户可去掉后重发）
 │    └─ 全部 OK → base64 只在此刻生成（不落盘、不进历史）
 ├─ 合计 > max_message_bytes 或超 token 预算？
 │    └─ 按策略裁剪（默认「先大后小」；可选「先旧后新」）+ 插入可见降级说明
 └─ 落盘时超会话配额？
      ├─ 先跑一次未引用媒体 GC（CollectMedia，dryRun 先报告）
      ├─ 仍超 → 拒绝新图 + 给出 `/media` 清理入口
      └─ 绝不自动删「仍被引用」的旧图（会破坏可回放性）
```

补充语义：

- **provider 不支持图片**（无 vision 能力标记）：显式报错并给出两条出路
  （换账号 / 去掉图片），不做「保存了但没发出去」的假成功。
- **降采样必须回填元数据**：`Scale`、`Width/Height` 写进 `AttachmentRef`，
  前端显示「已缩放至 2048×1152」——用户能看到成本被压下来了。
- **幂等**：同一 `media:<hash>` 重复附件按一份计（媒体分区已内容寻址）。
- **拒绝的错误码要可编程**：前端据此把「超张数」渲染成可交互的「去掉 N 张」，
  而不是一段无法操作的错误文本。

### 5.3 视觉 token 计价（限流与预算的前提）

现状 `seelexctx/tokens/tokens.go:41` 是字符公式，图片计 0 分，**必须补**：

```text
tiles(w,h) = ceil(w/512) * ceil(h/512)
imageTokens ≈ 85 + 170 * tiles(w,h)      // OpenAI 风格小块计价
detail=low  → 先缩到 512 长边，tiles = 1
```

标定证据（本仓库实测，非引用文档）：`seelebridge/multimodal` 真机冒烟
（128×128 PNG + 约 50 个中文字符提示）返回 `prompt_tokens = 249`；纯文本部分按
`tokens.go:41` 估算约 60-75，故图片实付约 **175-190 token ≈ 1 个 tile** 的量级，
与上式 `85+170×1 = 255` 同阶。**结论：图片成本由像素/tile 决定，与字节无关**——
这正是它必须与文本「按字符」分轴计量、以及 `detail` 必须成为显式成本杠杆的依据。

### 5.4 并发与限流

| 维度 | 现状 | 方案 |
|---|---|---|
| 账号并发 | 已有租约信号量（`accountpool/pool.go:21-28,168-202`） | **加权租约**：一次带图请求消耗 `1 + Σ图片 × w` 个槽（`multimodal_vision_weight` 默认 0.5）——8 图请求 = 5 槽，避免「8 图请求与 1 文本请求抢同一档并发」 |
| 请求速率 | **无**（已核实：api 无重试、pool 无 RPM/TPM） | 每账号令牌桶：`multimodal_image_rpm`（默认 30）/ `multimodal_image_tpm`（按 §5.3 计费），超桶 → 排队而非失败 |
| 429/限流响应 | 无退避（仅 MCP 熔断有，`tools/mcp/breaker.go`） | 尊重 `Retry-After`；否则抖动指数退避（base 1s、max 60s、上限重试 3 次）；把「排队中/第 k 次重试」作为事件推给前端，避免用户以为卡死 |
| 队列 | `InputQueue.Enqueue` 无上限（`session/ports.go:59-81`） | 有界队列（默认 8 条）+ 满则显式拒绝；队列只存引用（§4.1 规则 1） |
| 预处理并发 | 无（降采样/编码是 CPU 活） | 有界工作池 `min(4, GOMAXPROCS)`；编码结果按 `(ref, detail)` 缓存，重试不重编码 |
| 成本控制 | 无 | 历史图片窗口 `multimodal_history_turns`（默认 3）：超窗的图片 part 替换为占位符，保留前 N 轮的图不重复计费 |
| 观测 | 无图片计量 | 记录每次请求的「图片数 / 估算 vs 实付 token / request_bytes / 降采样次数」，用于回填 §5.1 默认值 |

### 5.5 与既有机制的关系

- **审批流**（approval broker）不用于图片：上传是用户主动行为，加审批只会伤体验；
  但工作区外路径读取（§4.7）建议在 GUI/CLI 首次使用时给一次**显式确认文案**。
- **会话媒体配额**（已实现）与**请求限额**是两层：前者管磁盘、后者管 provider 成本，
  错误码与文案必须分开，避免用户把「磁盘满了」误读成「模型不收图」。
- **subagent/fork**：子代理不继承父会话的图片（避免成本乘性放大），需要时由父显式
  把结论（而非原图）写进子任务描述。

---

## 6. 分阶段落地与验收

### P0（能不依赖上游先做，价值最高）

1. seelex：`EngineMessage.Parts` + 适配器映射（S4、§4.5）——纯新增字段，旧路径不变。
2. seelex：`/attach` 命令 + 媒体分区落盘 + `/media` 列表（S3、S11、§4.2）。
3. seelex：额度四轴 + 超限阶梯的第 1/2 级（拒绝并指名、自动降采样），错误码可编程（§5.2）。
4. 测试：`/attach` 的路径/格式/超限表驱动；媒体分区「历史 JSON 不含 base64」断言。
   验收：`go test ./application/... ./sessionstore/... -count=1` 全绿；
   真机冒烟复用 `seelebridge/multimodal` 的 opt-in 用例。

### P1（依赖上游 §3 的 PR-1/PR-2）

5. Seele：`Message.Parts` + 自定义序列化 + `MediaResolver` + 两个 strategy 映射。
6. seelex：装配 `MediaResolver`（适配 `MediaStore`），`Submit` 带附件路径打通到引擎。
7. 限流：加权租约 + RPM/TPM 令牌桶 + 429 退避（§5.4）。
8. 验收测试（必须包含）：
   - 旧 JSON 往返逐字节等价（`MarshalJSON` 回归）；
   - `Content=nil && Parts=nil` 与「只发文本」两条路径请求体与改造前一致；
   - 带图历史 < 1 MB（`seelectx/cache` 1 MB 拒写边界）；
   - 图片数超限 → 请求体图片数 == 上限且降级说明可见（真机 + 假服务端各一次）；
   - resolver 缺失 → `ErrMediaResolverMissing`（不静默降级）。

### P2（体验与成本精修）

9. GUI 文件选择/拖放/缩略图/进度；TUI chips。
10. 图片剪贴板粘贴（平台专用实现）；Responses API `input_image` 分支。
11. 图片保留窗口与成本报表落地，用实测 usage 回填 §5.1 默认值。

### 回滚点

- P0 全部是**新增字段与新增命令**：回滚 = 移除命令注册 + 忽略 `Parts`，旧路径不受影响。
- P1 的回滚开关：`multimodal_enabled=false` 时 `Parts` 一律折叠为文本占位符，
  请求体与今天逐字节一致（这是上游 PR 的必测项，也是灰度依据）。

---

## 7. 风险与取舍

| 风险 | 说明 | 处置 |
|---|---|---|
| 自定义序列化破坏兼容 | `types.Message` 被大量直接复用为请求/响应结构体（B2） | 只在 `len(Parts) > 0` 时分叉；无 parts 走 `type legacy` 别名；加逐字节等价回归 |
| 上游不接受改动 | Seele 是外部依赖 | 退路：`api` 层新增 `MessageWire` DTO 只在 strategy 内转换；更差的退路是 seelex 自建带图客户端（已存在 `seelebridge/multimodal`，但等于脱离引擎） |
| 大图 token 失控 | 图片按 tile 计价，4K 全屏截图 ≈ 数十 tile | `detail` 杠杆 + 长边 clamp + 历史窗口；三者都要有，缺一个就有成本事故面 |
| 静默丢图 | 本方案最想消除的失效模式 | 所有降级都写入可见文本；无 resolver / 无 vision 能力 → 显式错误（§3.4、§5.2） |
| 磁盘/内存放大 | 队列无上限（S6）、8×5 MB base64 字符串 | 先落盘后入队、队列有界、编码结果缓存、`Payload` 只带引用 |
| 路径门禁被绕过 | `PathGate` 无调用点（S9） | `/attach` 是人类输入面专用；工具不得获得该入口；先清理死代码或明确其定位 |

---

## 8. 附录

### 8.1 证据索引（本文所有 file:line 均经只读核对）

- Seele v0.1.3（模块内相对路径）：`types/model.go:11-16,23-40,57-65,68-78`、
  `agent/core/api/strategy_openai.go:16,25,42`、`agent/core/api/strategy_anthropic.go:277-347`、
  `session/loop.go:18,165,224,537`、`seelectx/cache/config.go:38`、
  `seelectx/cache/filecache.go:139,156`、`seelectx/ctx_manager/history.go:167,205,213,221`、
  `accountpool/pool.go:21-28,168-202`、`accountpool/selector.go:21-29,36-85`、
  `tools/mcp/breaker.go:44-55,93,122-126`、`docs/plan/07-v05-roadmap-gap-analysis.md:63,337`。
- seelex（仓库内相对路径）：`application/core/service_input.go:64-79`、
  `application/core/input_router/router.go:26-31,44-50`、`application/core/command.go:11-19`、
  `application/core/skill_context.go:16-21`、`application/contract/ports.go:17-24`、
  `internal/adapters/messages.go:15-32`、`application/model/state.go:101-112`、
  `session/ports.go:59-81`、`workspace/readfile.go:33-77`、`gui/dialogs_gui.go:9-11`、
  `gui/frontend/dist/app.js:2016-2021`、`seelebridge/security/project_scope.go:124-130,134-152`、
  `seelebridge/account/account.go:30-38,49-55,106-124`、`seelebridge/account/manager.go:155-168,180-203`、
  `seelexctx/tokens/tokens.go:23,41,57`、`sessionstore/media.go`、`seelebridge/multimodal/`。

### 8.2 默认值一览（P0/P1 落地时写入 `config/seelex.yaml` 注释与 `seelexctx/limits.go`）

```yaml
limits:
  session_storage:
    multimodal_max_images: 8            # 单条消息张数
    multimodal_max_message_bytes: 25165824   # 24 MiB，单条合计
    multimodal_wire_image_bytes: 5242880     # 5 MiB，provider 侧单张
    multimodal_max_item_bytes: 8388608       # 8 MiB，落盘单张（已实现）
    multimodal_max_long_side: 4096           # 像素长边（已实现）
    multimodal_session_quota_bytes: 268435456 # 256 MiB（已实现）
    multimodal_history_turns: 3              # 历史保留图片的轮数
    multimodal_vision_weight: 0.5             # 每张图折算的并发租约槽
    multimodal_image_rpm: 30                  # 每账号每分钟图片请求
    multimodal_image_tpm: 200000              # 每账号每分钟视觉 token
    input_queue_max_items: 8                  # 有界队列（现无上限）
```
