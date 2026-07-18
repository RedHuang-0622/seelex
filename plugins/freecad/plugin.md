---
schema_version: 1
name: freecad
description: FreeCAD 计算机辅助设计 — 通过 Robust MCP Server 接入
include: [switch_plugin, switch_mode, get_time]
exclude: []
mcp_servers:
  - name: freecad
    transport: stdio
    command: freecad-mcp
    args: ["--mode", "xmlrpc"]
---

# FreeCAD 计算机辅助设计

通过 [Robust MCP Server](https://github.com/spkane/freecad-addon-robust-mcp-server) 连接 FreeCAD（150+ 工具）。

## 前提

- FreeCAD ≥ 0.21 已安装
- `freecad-robust-mcp` 已安装：`pip install freecad-robust-mcp`
- Robust MCP Bridge workbench 已安装（FreeCAD Addon Manager → "FreeCAD Robust MCP Suite"）

## 使用方式

1. 打开 FreeCAD → Robust MCP Bridge 工作台 → **Start MCP Bridge**
2. 在 Seelex 中激活插件：
   ```
   switch_plugin freecad
   ```
3. LLM 自动使用 CAD 工具（导入 STEP、草图、拉伸、开孔等）

## 设计原则

- 始终先澄清设计目标和约束（材料、尺寸公差、装配要求）
- 单位系统默认为 mm
- 每次 MCP 调用都会被 `mcpstack` 记录，可追溯、可查看
- 复杂设计分步执行，每步确认结果
- 设计完成后提示用户导出或保存
