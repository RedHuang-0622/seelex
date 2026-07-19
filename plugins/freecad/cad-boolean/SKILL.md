---
description: 批量布尔操作 — 多工具一次性切割，避免逐条布尔导致 recompute 堆积超时
---

# 批量布尔操作

依赖 `cad-core/freecad_core.py` 加载后使用，或直接导入本目录脚本。

## 模式

逐条布尔切割每次触发一次 recompute（~2s），N 个对象切 N 次 = 2N 秒 + 超时风险。

批量方案：创建全部工具体 → MultiFuse 融合 → 一次 Cut → 一次 recompute。

从 **2N 次 recompute** 降到 **1 次**。

## 何时用

- 法兰螺栓孔（6+ 个均布孔）
- 散热孔阵列（N 个矩形孔网格排列）
- 减重槽（多个槽体同时切割）
- 任何需要多个布尔切割的场景

## 使用方式

### 方式一：cad-core 内置函数

```python
exec(open('G:/Program/go/seelex/plugins/freecad/cad-core/freecad_core.py').read())

# multi_cut 已包含在 cad-core 中
tools = [
    {"type": "cylinder", "name": "Hole_1", "radius": 6, "height": 20, "center": (30, 0, 0)},
    {"type": "box", "name": "Slot_1", "length": 10, "width": 5, "height": 20, "center": (-20, 0, 0)},
]
result = multi_cut(doc, "Base", tools, "Result")
```

### 方式二：独立脚本

| 脚本 | 用途 |
|------|------|
| `batch_boolean_cut.py` | 多工具熔合后一次性切割（独立版） |
| `batch_placement.py` | 圆周阵列 / 直线阵列 / 镜像 |

阵列操作也可用 cad-core 的 `circular_array()` / `linear_array()`。
