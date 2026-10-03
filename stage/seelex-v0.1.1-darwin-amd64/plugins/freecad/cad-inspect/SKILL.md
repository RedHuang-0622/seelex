---
description: 对象诊断 — 检查文档状态、对象属性、体积/质量计算
---

# 对象诊断与测量

依赖 `cad-core/freecad_core.py` 的 `list_all()`, `inspect_obj()`, `get_volume()`, `get_mass()`, `check_exists()`, `get_bounds()`, `classify_edges()`。

## 快速诊断

```python
exec(open('G:/Program/go/seelex/plugins/freecad/cad-core/freecad_core.py').read())

# 列出所有对象
list_all()

# 详细检查
inspect_obj(doc, "TargetObject")

# 包围盒
get_bounds(doc, "TargetObject")
```

## 操作前验证

```python
if not check_exists(doc, "TargetObject"):
    print("FAIL: object not found, run list_all() first")

# 检查形状有效性
obj = doc.getObject("TargetObject")
if obj.Shape and not obj.Shape.isValid():
    print("WARN: invalid shape, try recompute()")
```

## 操作后验证

```python
vol = get_volume(doc, "Result")
mass = get_mass(doc, "Result", density=7.85e-6)  # 45钢
```

## 密度参考

| 材料 | 密度 kg/mm³ |
|------|:----------:|
| 45 钢 | 7.85e-6 |
| 6061 铝合金 | 2.70e-6 |
| ABS 塑料 | 1.04e-6 |
| 铜 | 8.96e-6 |

## 独立脚本

| 脚本 | 用途 |
|------|------|
| `inspect_objects.py` | 列出所有文档对象及其属性/体积（独立版） |
| `edges.py` | 按半径/长度/类型筛选和分类边 |
| `check.py` | 清理脏对象 + 对象存在性检查 |
| `test.py` | 快速诊断测试 |

> 注意：`tools_raw.txt` 是旧 MCP 工具列表，MCP 已移除，仅供参考。
