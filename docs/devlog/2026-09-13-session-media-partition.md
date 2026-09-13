# 2026-09-13 会话媒体分区基建 + 多模态真机冒烟

> 日期: 2026-09-13 | 范围: `sessionstore/media.go`（新）、`sessionstore/storage_settings.go`、
> `seelexctx/limits.go`、`main.go`、`config/seelex.yaml`、`seelebridge/multimodal/`（新）

## 一、背景与边界

用户明确了两件事的归属：**图片识别工具不拷贝图片**（只读路径，路径失效显式报错），
**截屏工具才做拷贝**，且拷贝结果进会话粒度的「大工具结果」通道，图片本身挂到多模态
结果字段下（英文命名），目录用 hash、保留原文件名；同时要求多模态与普通大结果
**在阈值上分轴**——「5 MB 纯文本很吓人，5 MB 图片很正常」。

实现前先核实了三处差距（代码是唯一事实来源）：

1. `hash 文件夹 + 保留原名` 只写在 `docs/2026-09-08-session-storage-architecture/my_design.md`
   §10（`session/meta/<hash>/<原名>`），**未实现**；已实现的是扁平
   `big_tool_result/<hash>.jsonl`。
2. `sessionstore/big_tool_result.go` 的 `toolBlob.Content` 是纯文本通道且带
   `content[:softChars]` 截断：二进制塞不进 JSONL，截断后的 PNG 是坏文件，且图片
   会和文本大结果抢同一份 64 MB 会话配额。
3. Seele v0.1.3 的 `types.Message.Content` 是 `*string`（纯文本），`strategy_openai.go`
   的 `Content string` 决定了**带图请求无法经引擎下发**——这是外部依赖的能力缺口，
   不是本仓库能通过改接线解决的。

因此本次落地的「原子能力基建」= 媒体分区（存储）+ 多模态引用字段 + wire 编码，
真机冒烟证明「落盘 → 引用 → 读回 → provider 识图」整条链路可用。

## 二、实现

### sessionstore 媒体分区（`media.go`）

```text
<sessionRoot>/meta/<sha256>/<原名>      二进制原文（永不截断）
<sessionRoot>/meta/<sha256>/meta.json   单条索引（MediaRef）
<sessionRoot>/metadata/media.json       会话级索引 head（可重建派生）
```

- 内容寻址：目录名 = 内容 sha256（同一份字节只落一份，换名写入记为 `aliases`）；
  文件名保留写入方给的原名，仅剥离目录与文件系统非法字符、Windows 保留设备名加前缀。
- 引用 `media:<sha256>`；`MediaStore` 作为**可选能力接口**（`MediaStoreOf` 取用），
  不进 `Repository` 主契约，避免强迫所有后端与测试替身实现。
- `ToolResult.Multimodal []MediaRef`（JSON `multimodal`）：工具结果只挂引用，不复制字节；
  `ReferencedMediaHashes` 从工具结果收集 GC 引用集。
- 尺寸校验用调用方给的 `Width/Height`（存储层不解码图片），所以「先降采样再写」是
  调用方责任（复用截屏原语已有的 `MaxWidth` + `ScaleNearest`）。

### 阈值分轴（`storage_settings.go` / `limits.go` / `main.go` / `seelex.yaml`）

| 维度 | 文本大结果 | 媒体 |
|---|---|---|
| 软限 | 60000 chars（截断 + `result_ref`） | **无** |
| 单件硬限 | 16 MB | 8 MB（`media_max_item_bytes`） |
| 像素 | 不适用 | 长边 4096（`media_max_long_side`） |
| 条目数 | 不适用 | 500（`media_max_items_per_session`） |
| 会话配额 | 64 MB | 256 MB（`media_session_quota_bytes`），独立计账 |

不变式：**文本可截断，媒体永不截断**。配额只统计二进制载荷，`meta.json` 不计费。
新增 `validateStorageSettings` 校验：媒体限额必须 > 0，且配额须覆盖单件上限。

### seelebridge/multimodal（wire 适配）

- `BuildParts`/`BuildMessage`：文本 + `image_url` data URL；
- `CompleteWithImages`：最小带图同步客户端（base_url 兼容带/不带 `/v1` 与尾斜杠），
  wire 层图片上限 5 MB（低于落盘 8 MB：data URL 还要再涨 33%）；
- `Answer.FinalText()`：content 为空时回退 `reasoning_content`（见下）。

## 三、验证证据

### 单元测试

```text
go test ./sessionstore/ -run TestMedia -count=1     # 布局契约/永不截断往返/四条限额轴/去重/GC/配额独立
go test ./seelebridge/multimodal/ -count=1          # 编码形状/base_url 归一化/假服务端请求与失败语义
```

### 真机冒烟（opt-in，真实 DeepSeek API）

```text
$env:SEELEX_LIVE_SMOKE='1'
$env:SEELEX_ACCOUNTS_PATH='<path>/accounts.yaml'
go test ./seelebridge/multimodal/ -run TestLiveMultimodalImageSmoke -count=1 -v -timeout 300s
```

结果：

```text
live target: model=deepseek-v4-flash base_url=https://api.deepseek.com provider=openai
media: ref=media:9248226cfb24685e9f9c7a8ae532aefc8b9f8fd152ba588c7f5e81d5354a6f24
       name=...\session-547b6bd2b1304637\meta\9248226c...\prompt-smoke-20260913-102527.302.png
       bytes=367
answer: model=deepseek-flash finish=stop content="red blue" prompt_tokens=249 completion_tokens=80 request_bytes=836
--- PASS: TestLiveMultimodalImageSmoke
```

- 断言选的 `red`/`blue` **不出现在文字提示里**：通过即证明图片字节确实送达模型，
  链路任一段静默丢图（空回答、只发文本、被截断）都会失败。
- 落盘路径同时印证了「hash 做文件夹 + 保留原名」在真实盘上成立。

### 真机踩到的两个坑（已固化进测试）

1. **`max_tokens=64` 时 `message.content` 为空、`completion_tokens=64`**：思考模式把
   预算全用在 `reasoning_content` 上。只读 `content` 的客户端会得到空回答，并把它
   误判成「模型没看到图」——这正是多模态链路最容易误判的地方。
   处理：`Answer` 增加 `Reasoning`/`FinishReason`，`FinalText()` 在 content 为空时
   回退 reasoning；冒烟用 512 预算并显式断言非空。
2. **协议可用性与模型名**：`deepseek-v4-flash` 现路由到 V4.1-Flash（`deepseek-flash`），
   chat/completions 接受 content parts（观测 200 + 正确识别），无需改走 `/responses`。

## 四、未做 / 后续

- **引擎链路接入未做**：带图请求目前只能由本包直接发起。要让普通对话轮次也能带图，
  需要 Seele 侧把 `types.Message.Content` 从 `*string` 扩展为 content parts
  （外部依赖改动，本仓库无法单方面完成）。
- **截屏工具未落地**：`seelebridge/tools/computer/README.md` 明确该包「不读取或修改
  Seelex 会话」「不注册进 Seelex 的工具」，所以截屏工具应建在 Seelex 侧、复用原语的
  `MaxWidth`/`ScaleNearest`，落 `meta/<hash>/<原名>` 并回 `multimodal` 引用；
  MCP 服务端继续服务宿主机 Codex，两份拷贝互不影响。
- **application DTO 未透出**：`multimodal` 目前落在存储记录（`ToolResult.Multimodal`）与
  wire 层，尚未进 `application/contract/dto` 与前端展示。
- **阈值待实测校准**：8 MB / 4096 px / 256 MB 是按 4K 截屏体积量级给出的建议值，
  图片 token 成本只做定性论证（由像素/tile 决定），建议按真实 usage 回填。
