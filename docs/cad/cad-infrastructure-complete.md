# CAD 基建现状

> **⚠️ 本文已过时，保留仅作历史参考**
>
> 本文的「双栈」方案已被重构：`commandstack/` → 通用 `mcpstack/` 中间件，`freecad/server/` → 改用现成的 FreeCAD MCP Server。
> 当前架构见 [`../arch/design-decisions-mcp-storage.md`](../arch/design-decisions-mcp-storage.md)。
> 下文保留原始内容供追溯。

---

## 原始内容（2026-07-18）

### 两个栈的基建总览（旧方案）

```
G:\Program\go\seelex\
│
├── commandstack/            ← Stack 1: Go 命令栈 (SSOT) — **已删除**
│   ├── commandstack.go       — 类型定义 (CommandStack, Command, CommandMetadata)
│   ├── ops.go                — 操作常量 (22 种 CAD 操作)
│   ├── stack.go              — Stack 接口 + Push/Undo/Redo/Current/Peek
│   ├── persist.go            — Save/Load (原子写入)
│   ├── prompt.go             — ForPrompt (Token 预算感知的 LLM 摘要)
│   ├── snapshot.go           — Snapshot 深度拷贝
│   ├── provider.go           — Provider 接口实现 (Export → ContextSnapshot)
│   ├── stack_test.go         — 14 个测试，全部通过
│
├── freecad/                 ← Stack 2: Go 类型/Schema + Python MCP Server
│   ├── freecad.go            — 包声明 + 操作常量 (Alias from commandstack)
│   ├── ops.go                — 15 种 CAD 操作的 Go 参数类型定义
│   ├── schema.go             — AllSchemas 注册表
│   ├── validate.go           — Validate 函数 (含边界验证)
│   │
│   └── server/               ← Python MCP Server (实际执行层) — **已删除**
│       ├── __init__.py
│       ├── server.py          — MCP Server 主入口 (JSON-RPC 循环)
│       ├── mcp_protocol.py    — MCP 协议层 (initialize/tools/list/tools/call)
│       ├── freecad_manager.py — FreeCAD 文档生命周期管理
│       ├── utils.py           — 日志与调试工具
│       ├── requirements.txt   — 无外部依赖 (仅 FreeCAD + stdlib)
│       └── operations/
│           ├── __init__.py
│           ├── sketches.py     — 草图操作 (矩形/圆形/线段)
│           ├── features.py     — 3D 特征 (拉伸/开孔/旋转)
│           ├── modifiers.py    — 修饰 (圆角/倒角/镜像)
│           └── io.py           — I/O (STL/STEP/FCStd)
│
└── plugins/freecad/          ← Plugin 定义
    └── plugin.md             — MCP Server 声明 + include: [cad_*] + 系统提示词
```

---

## 当前实际架构（2026-07-18 重构后）

```
seelex/
│
├── mcpstack/                 ← 通用 MCP trace 中间件（不限于 CAD）
│   ├── stack.go               — MCPCall, MCPStack, Record/Undo/Redo/Peek/查询
│   ├── interceptor.go         — CallRecorder: BeforeCall/AfterCall 生命周期
│   ├── persist.go             — 原子 Save/Load（.seelex/mcp-traces/）
│   ├── prompt.go              — ForPrompt(budget) 给 LLM 喂上下文
│   ├── provider.go            — TraceProvider.BuildSnapshot()
│   ├── snapshot.go            — 深拷贝
│   ├── breaker.go             — ListenBreaker() 消费熔断事件
│   └── stack_test.go          — 18 个测试
│
├── freecad/                  ← 仅做参数验证，不做 MCP 通信
│   ├── freecad.go             — CAD 操作常量（已去别名，独立定义）
│   ├── ops.go                 — 16 种 CAD 参数类型
│   ├── schema.go              — AllSchemas 注册表
│   ├── validate.go            — Validate 函数（含边界验证）
│   └── validator.go           — PreValidate/PostValidate 中间件函数
│
├── seelebridge/
│   ├── mcp.go                 — AttachMCP(): 熔断事件 channel + ListenBreaker + Provider.Attach
│   ├── runtime.go             — Runtime 持有 MCPStack, StorePath
│   └── storage.go             — SessionStore 封装 storage.Storage 接口
│
├── mcpconfig.go              — registerMCPServers(): 读取 account YAML 注册 MCP
│
├── config/account-openai.yaml
│   └── mcp_servers:            ← FreeCAD 在这里配置（跟 WebSearch 同等级）
│       - name: freecad
│         transport: stdio
│         command: FreeCADCmd
│
└── plugins/freecad/
    └── plugin.md              — 指向现有 FreeCAD MCP Server，不自研
```

### 关键变化

| 旧方案 | 新方案 | 原因 |
|--------|--------|------|
| `commandstack/` CAD 专属 | `mcpstack/` 通用中间件 | 所有 MCP 调用都需要追溯，不应绑定 CAD |
| `freecad/server/` 自研 Python | 外部 FreeCAD MCP Server | 已有现成实现，不自研轮子 |
| `seelebridge` 无栈持有 | `Runtime.MCPStack` | 栈是全局中间件，由桥接层管理 |
| 熔断事件不暴露 | `breaker.emit()` → channel → `mcpstack` | trace 有盲区，记录不了熔断原因 |
| 存储硬编码 `.seele/` | `Storage` 接口 + `FileStore` | 解耦，应用选实现 |
| 框架默认路径 `.seele/sessions/` | 空路径 = no-op | 不偷建目录 |
