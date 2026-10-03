---
description: 批量执行 — 接受 JSON 操作序列，一次性在 FreeCAD 中执行全部操作。MCP 的兜底方案，同时省 token。
---

# 批量执行

**核心目的：省 token + 降 MCP 故障率。**

MCP 逐条调用 = 50 次往返 + 50 次 recompute。批量执行 = 1 次 bash 调用 + 全部体素创建 + 1 次 recompute。

## 用法

```bash
FreeCADCmd G:/Program/go/seelex/plugins/freecad/cad-batch/cad_batch.py \
  --params C:/temp/cad_params.json \
  --output C:/temp/result.step
```

## 参数文件格式

JSON，顶层 `operations` 数组。每个操作一个 type：

```json
{
  "operations": [
    {"type": "box",     "name": "Base",   "length": 100, "width": 50, "height": 20},
    {"type": "cylinder","name": "Hole",   "radius": 10,  "height": 30, "center": [50,25,20]},
    {"type": "cut",     "name": "Result", "base": "Base", "tool": "Hole"},
    {"type": "fillet",  "base": "Result", "radius": 3,   "edge_filter": {"min_length": 10}},
    {"type": "export_step", "objects": ["Result"]}
  ]
}
```

### 支持的操作

| type | 参数 | 说明 |
|------|------|------|
| `box` | name, length, width, height, center? | 长方体 |
| `cylinder` | name, radius, height, center? | 圆柱体 |
| `sphere` | name, radius, center? | 球体 |
| `cone` | name, radius1, radius2, height, center? | 圆锥/圆台 |
| `torus` | name, radius1, radius2, center? | 圆环 |
| `helix` | name, pitch, height, radius, center? | 螺旋线 |
| `cut` | name, base, tool | 布尔剪切 |
| `fuse` | name, base, tool | 布尔融合 |
| `common` | name, base, tool | 布尔交集 |
| `multi_cut` | name, base, tools[{type,name,...}] | 多工具一次性剪切 |
| `circular_array` | template, count, radius, start_angle?, name_prefix? | 圆周阵列 |
| `linear_array` | template, count, step, direction?, name_prefix? | 直线阵列 |
| `mirror` | source, plane, offset? | 镜像 |
| `move` | name, x, y, z | 绝对定位 |
| `translate` | name, dx, dy, dz | 相对平移 |
| `rotate` | name, axis, angle, center? | 旋转 |
| `fillet` | base, radius, edge_filter? | 圆角（edge_filter: min_length/max_length/min_r/max_r） |
| `chamfer` | base, radius, edge_filter? | 倒角 |
| `delete` | name | 删除对象 |
| `hide` | name | 隐藏 |
| `show` | name | 显示 |
| `save` | path? | 保存文档 |
| `export_step` | objects[], out_path? | 导出 STEP |
| `export_stl` | objects[], out_path? | 导出 STL |
| `inspect` | name? | 诊断输出 |

## 何时用

- **LLM 判断 MCP 逐条调用会超过 10 次往返** → 生成 JSON 参数文件 → 调 batch 一次执行
- **MCP 连续 2 次超时/失败** → 自动切换到 batch 兜底
- **需要输出完整零件** → batch 生成 + 导出一步到位

## 与 MCP 的关系

```
首选 MCP（逐步操作，交互性好）
  ↓ 失败/超时
batch 兜底（JSON 驱动，一次执行）
  ↓ 也失败
bash 裸调 FreeCADCmd（最后手段）
```
