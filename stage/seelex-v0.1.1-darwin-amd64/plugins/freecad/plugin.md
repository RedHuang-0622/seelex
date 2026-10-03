---
schema_version: 1
name: freecad
description: FreeCAD 计算机辅助设计
include: [switch_plugin, switch_mode, get_time, read_file, grep_search, glob, "write*", "edit*", bash]
exclude: []
mcp_servers:
  - name: freecad
    transport: stdio
    command: C:\Users\redre\anaconda3\Scripts\freecad-mcp.exe
    args: ["--mode", "xmlrpc"]
    env:
      - "PYTHONIOENCODING=utf-8"
      - "FREECAD_TIMEOUT_MS=120000"
---

# FreeCAD 计算机辅助设计

## 操作策略（重要）

```
MCP（首选）→ 逐步交互，灵活探索
  ↓ 超过 10 次调用 / 连续 2 次超时
Batch（兜底）→ JSON 驱动，一次执行，省 token
  ↓ 也失败
Bash FreeCADCmd（最后手段）
```

## 策略判断

| 场景 | 用 MCP | 用 Batch |
|------|:------:|:--------:|
| 查看文档对象列表 | ✅ | |
| 创建 1-3 个体素 | ✅ | |
| 单次布尔运算 | ✅ | |
| 移动/旋转/删除单个对象 | ✅ | |
| **创建 4+ 个相同类型体素**（如 6 个螺栓孔） | | ✅ `cad_batch.py` |
| **多对象布尔剪切** | | ✅ `cad_batch.py` |
| **完整零件生成**（读取设计规格 → 建模 → 导出） | | ✅ `cad_batch.py` |
| MCP 连续 2 次超时 | | ✅ 自动切换 `cad_batch.py` |

## Batch 用法

```bash
# 1. 用 write_file 写入参数 JSON
# 2. 用 bash 执行
FreeCADCmd G:/Program/go/seelex/plugins/freecad/cad-batch/cad_batch.py \
  --params C:/temp/cad_params.json \
  --output C:/temp/result.step
```

参数格式见 `cad-batch/SKILL.md`。LLM 生成 JSON 参数文件 → bash 调用 `cad_batch.py` → 返回结果。

JSON 操作序列比直接生成 Python 代码省 80% token。

## 核心库

`cad-core/freecad_core.py` — 40+ 函数：

| 类别 | 函数 |
|------|------|
| 文档 | `new_doc`, `get_doc`, `save_doc`, `recompute`, `undo`, `redo`, `clear_all` |
| 体素 | `make_box`, `make_cylinder`, `make_sphere`, `make_cone`, `make_torus`, `make_helix`, `make_regular_polygon`, `make_feature` |
| 布尔 | `bool_cut`, `bool_fuse`, `bool_common`, `multi_fuse`, `multi_cut` |
| 变换 | `move_to`, `translate`, `rotate_obj`, `mirror_obj`, `scale_obj`, `copy_obj`, `delete_obj`, `hide_obj`, `show_obj`, `set_placement` |
| 阵列 | `circular_array`, `linear_array` |
| 导出 | `export_step`, `export_stl` |
| 诊断 | `list_all`, `inspect_obj`, `get_volume`, `get_mass`, `check_exists`, `get_bounds`, `classify_edges` |

## 建模原则

1. **体素 + 布尔** — Part 工作流
2. **先创建后合并** — 全部体素创建 → 一次 MultiFuse → 一次 recompute
3. **循环外 recompute** — 绝对不要在循环里 recompute
4. **操作超时不熔断** — `context.DeadlineExceeded` ≠ 连接故障
5. **用 FreeCADCmd（headless）** — 不要用 FreeCADGui

## 技能导航

| Skill | 用途 |
|-------|------|
| `cad-core` | 🔑 核心操作库 |
| `cad-batch` | ⚡ 批量执行（MCP 兜底 + 省 token） |
| `cad-boolean` | 批量布尔剪切、阵列 |
| `cad-fillet` | 分批圆角 |
| `cad-inspect` | 对象诊断 |
| `cad-repair` | 修复恢复 |
| `cad-template` | 参数化模板参考 |
