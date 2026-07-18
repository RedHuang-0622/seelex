---
schema_version: 1
name: freecad
description: FreeCAD 计算机辅助设计 — 通过现有 FreeCAD MCP Server 接入
include: [switch_plugin, switch_mode, get_time]
exclude: []
mcp_servers:
  # FreeCAD MCP Server 配置
  # 参考：https://github.com/FreeCAD/FreeCAD-MCP 或社区 MCP 实现
  # 用户需根据实际安装路径调整 command 和 args
  - name: freecad
    transport: stdio
    command: FreeCADCmd
    args: [-c, freecad_mcp_server.py]
    env:
      - FREECAD_USE_HEADLESS=1
---

# FreeCAD 计算机辅助设计

通过 MCP 协议连接到 headless FreeCAD 实例。

## 插件说明

本插件使用通用 MCP 中间件（`mcpstack`）记录所有 CAD 操作的调用历史，
并在调用前进行参数合法性验证（防止无效几何参数到达 FreeCAD 内核）。

## 使用方式

1. 确保 FreeCAD ≥ 0.21 已安装且 `FreeCADCmd` 在 PATH 中
2. 安装并配置一个 FreeCAD MCP Server（社区已有现成实现）
3. 在 Seelex 中激活插件：
   ```
   switch_plugin freecad
   ```
4. LLM 自动使用 CAD 工具

## 设计原则

- 始终先澄清设计目标和约束（材料、尺寸公差、装配要求）
- 单位系统默认为 mm
- 每次 MCP 调用都会被 `mcpstack` 记录，可追溯、可查看
- 复杂设计分步执行，每步确认结果
- 设计完成后提示用户导出或保存
