# 2026-09-13 computer use：看图原语 + 「查看当前页面状态」复合工具（MCP 面）

> 日期: 2026-09-13 | 范围: `seelebridge/tools/computer/view.go`（新）、
> `view_test.go`（新）、`computer/mcp/main.go`（新增两个工具）、
> `mcp/main_test.go`（新）、`seelebridge/tools/computer/README.md`

## 一、要解决的问题

base64 直接铺进上下文是浪费（宿主只有 200k 上下文，不该做「为了一时便利的无用功」）。
采取的形态是**工具调用**：入参地址、出参图片内容。落到 computer use 上就是两件事：

1. 一台**看图**原语：给路径、出图像内容，只读不拷贝；
2. 一条**复合**工具：把「截屏」与「查看画面 + 当前状态」绑成一次调用，模型不用自己
   串两次工具调用、也不用来回传路径。

## 二、实现

### 看图原语（`view.go`，平台无关）

`ViewImageFile(ViewImageOptions) (ViewedImage, error)`：

```text
Stat → 读取上限 32 MB → 读入 → image.Decode（PNG/JPEG/GIF 嗅探）
     → 不超宽：原样返回文件原文，Reencoded=false
     → 超宽：最近邻降采样 + 重编码 PNG（Reencoded=true，Scale<1）
```

只读语义是硬约束：不落盘、不拷贝、不改写原文件；失败原因分成四类，都可 `errors.Is`：
`ErrImageNotFound` / `ErrImageNotFile` / `ErrImageEmpty` / `ErrImageFormatUnsupported`
（另有 `ErrImageTooLargeToRead`）。这与截图原语的分工是刻意的——**截图产出资产（拷贝），
看图消费任意路径（不拷贝）**，所以看图在路径失效时只能显式报错，不能靠缓存兜底。

### MCP 工具面（`mcp/main.go`）

| 工具 | 形状 | 说明 |
|---|---|---|
| `view_screen` | 截屏 + 页面状态 | 复合工具：画面（region/max_width）与 `describeScreenState()` 的虚拟桌面、光标、前台窗口一次返回 |
| `view_image` | 路径 → 图像内容 | 只读看图；`inline_image=false` 只回路径与尺寸 |

- `describeScreenState()` 三段各自降级：某一项取不到只标注该项（`不可用(err)`），
  不让整次调用失败——模型要的是「画面 + 当前在哪」，任一项缺失不该把已拿到的信息一起丢掉。
- 内联封顶 `maxInlineImageBytes = 8 MB`：base64 还要再涨 33%，超限就回文本并提示
  「降低 max_width 后重试」，而不是把一行 JSON 撑到宿主读不回来。

## 三、验证

```text
go test ./seelebridge/tools/computer/... -count=1     # 两包全绿
```

- `view_test.go`：原样返回（`Data` 等于文件原文、`Scale=1`）、超宽降采样
  （800×400 → max_width=200 得 200×100、`Scale=0.25`、结果可解码为 PNG）、
  **看图不改写原文件**（重读文件字节与写入时一致）、五类失败各自 `errors.Is` 命中。
- `mcp/main_test.go`：`view_image` 的图像块 base64 可解码且为 320×240 PNG、
  `max_width=100` 得 100×50、`inline_image=false` 只回文本、缺 path / 文件不存在
  显式报错、`tools/list` 里 `view_screen` 的描述含「当前页面状态」。

真机 MCP 冒烟（`bin/computer-use-mcp.exe` 按 stdio 实际调用）：

```text
tools/list → 含 view_screen / view_image（描述非空）
view_screen {inline_image:false} → 当前页面状态
    虚拟桌面=1920x1080+0+0
    光标=(1324,150)
    前台窗口="Seelex" 1938x1098+-9+-9 句柄=0xAA0744 最小化=false
  + screenshot saved: …\shots\shot-20260913-204157.862.png（image=800x450 scale=0.417）
view_image {path=上述 PNG, max_width:200} → image=200x112 source=800x450 scale=0.250
  + 图像块 mimeType=image/png（base64 以 iVBORw0KGgo 开头 = PNG magic）
```

## 四、Seele 引擎侧的多模态能力现在是怎么绑定的（既然 `types.Message` 没变）

**结论：引擎侧没有绑定，一个字节都不过引擎；现在能用的是 Seelex 自建的旁路。**

证据（代码是唯一事实来源）：

- `Seele/types/model.go`：`Message.Content *string`，结构体里没有 attachments / parts 之类字段；
- `Seele/agent/core/api/strategy_openai.go:56`：请求侧 `Content string`，序列化就是裸字符串，
  content parts 无处安放；
- 整个 Seele 仓库 grep `image_url|multimodal|ContentParts|Attachments|image`（排除测试）**零命中**。

因此图片目前只绑在三处，且都在引擎之外：

| 层 | 绑定物 | 位置 |
|---|---|---|
| 存储 | 媒体分区 `meta/<hash>/<原名>` + `media:<sha256>` 引用 + `ToolResult.Multimodal` | `sessionstore/media.go`、`sessionstore/sessionstore.go:203` |
| wire | `media:` 引用读回字节 → OpenAI content parts（text + `image_url` data URL） | `seelebridge/multimodal/content.go`、`client.go` |
| 会话/展示 | 工具结果只挂引用；GC 用 `ReferencedMediaHashes` 收集引用集 | `sessionstore/media.go` |

含义：普通对话轮次**不能**带图——要带图，只有两条路：

1. Seelex 在引擎之外另发一次请求（现在的 `multimodal.CompleteWithImages`，一次性问答，不进对话循环）；
2. 把 Seele 的 `types.Message.Content` 从 `*string` 扩成 content parts，并让
   `strategy_openai.go`（以及 `strategy_anthropic.go` 的 `source.base64` 形态）跟着改
   —— 这是 Seele 仓库的改动，需要单独授权与独立版本（tag）承载。

## 五、未做 / 下一步（Seelex 侧）

- **Seelex 侧工具未落地**：应建在 Seelex 侧、复用原语 `MaxWidth`/`ScaleNearest`，
  截屏落 `meta/<hash>/<原名>` 并回 `multimodal` 引用（见
  `2026-09-13-session-media-partition.md` §四）。本次只做了 MCP（宿主 Codex）面，
  两份拷贝互不影响。
- **缺口一（本仓库可修）**：`seelebridge/tools/router.go` 的 `Deps.RegisterTool` handler
  签名是 `(ctx, argsJSON) (string, error)`——结果只有文本，没有承载媒体引用的通道。
  接缝候选：`application/core/tool_hooks.go:114 handleToolCompleteObserved`（拿得到
  `name/id/arguments/result`）与 `application/core/task_context/task_context_state.go:692
  storeToolResultLocked`（`ToolResult` 的构造点，`Multimodal` 就在那儿挂）。
- **缺口二（外部依赖）**：图片进模型要 Seele 引擎支持 content parts，见 §四。
- **缺口三**：`application/contract/dto` 尚未透出 `multimodal`，前端还看不到图。
- **labelme 出参未做**：精确点击需要「图片内容的坐标描述」（`shapes[].points` +
  `imagePath`/`Width`/`Height`），下一步可作为 `view_screen` 的坐标描述通道落地。
