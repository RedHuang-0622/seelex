# Internal Packages

## 生态位

`internal/` 存放只允许本 module 使用的基础实现。这里的代码不能被仓库外 import，也不应包含面向产品的 Application 用例。

当前子模块：

- [`buildinfo/`](buildinfo/README.md)：构建期注入的版本与默认前端。
- [`frontmatter/`](frontmatter/README.md)：Markdown YAML front matter 解析。
- （`promptassets/` 已于 2026-10-16 随 M5-R7 归位 [`kernel/prompt/assets`](../kernel/prompt/assets/README.md)：Seelex-owned 提示词资产，构建期嵌入。）
- [`testutil/`](testutil/README.md)：仅测试使用的共享桩（EmbeddedChatEngine 等）。
- [`winhide/`](winhide/README.md)：Windows 子进程控制台窗口隐藏（非 Windows 空操作）。

新增 internal 包前应确认它确实被多个内部模块复用；只被单一模块使用的 helper 优先留在拥有者目录。

## 架构图

```mermaid
flowchart LR
    OUT["仓库外代码"] -.->|禁止 import| INTERNAL["internal/"]
    INTERNAL --> B["buildinfo：版本注入"]
    INTERNAL --> F["frontmatter：front matter 解析"]
    INTERNAL --> P["kernel/prompt/assets：嵌入提示词"]
    INTERNAL --> T["testutil：测试桩"]
    INTERNAL --> W["winhide：子进程窗口隐藏"]
    P --> PROMPT["application/prompt / backend/tool/node"]
```
