---
description: FreeCAD 核心操作库 — 所有体素构造、布尔运算、变换、导出。替代 MCP，直接在 Python 控制台执行。
---

# FreeCAD 核心操作库

`freecad_core.py` 提供所有基础 CAD 操作，替代 MCP 的 83 个工具。全部使用 FreeCAD 原生 Python API，通过 Python 控制台粘贴执行。

## 加载

```python
exec(open('G:/Program/go/seelex/plugins/freecad/cad-core/freecad_core.py').read())
```

或在 FreeCAD 宏中导入。

## 函数速查

### 文档

| 函数 | 说明 |
|------|------|
| `new_doc(name)` | 创建新文档 |
| `get_doc(name?)` | 获取文档 |
| `save_doc(doc?, path?)` | 保存文档 |
| `recompute(doc?)` | 重新计算 |
| `undo(doc?)` / `redo(doc?)` | 撤销/重做 |
| `clear_all(doc?)` | 清空所有对象 |

### 体素构造

| 函数 | 说明 | 关键参数 |
|------|------|----------|
| `make_box(doc, name, l, w, h, center?)` | 长方体 | length, width, height, center=(x,y,z) |
| `make_cylinder(doc, name, r, h, center?)` | 圆柱体 | radius, height |
| `make_sphere(doc, name, r, center?)` | 球体 | radius |
| `make_cone(doc, name, r1, r2, h, center?)` | 圆锥/圆台 | radius1, radius2, height |
| `make_torus(doc, name, r1, r2, center?)` | 圆环 | radius1(环半径), radius2(管半径) |
| `make_helix(doc, name, pitch, h, r, center?)` | 螺旋线 | pitch, height, radius |
| `make_regular_polygon(doc, name, sides, r)` | 正多边形 | sides(边数), circumradius |
| `make_feature(doc, name, shape?)` | 通用 Feature | shape(TopoShape) |

### 布尔运算

| 函数 | 说明 |
|------|------|
| `bool_cut(doc, name, base, tool)` | 剪切: base - tool |
| `bool_fuse(doc, name, obj1, obj2)` | 融合: obj1 + obj2 |
| `bool_common(doc, name, obj1, obj2)` | 交集: obj1 ∩ obj2 |
| `multi_fuse(doc, name, obj_names[])` | 多对象融合 |
| `multi_cut(doc, base, tool_defs[], name)` | 批量剪切（单次 recompute） |

### 变换

| 函数 | 说明 |
|------|------|
| `move_to(doc, name, x, y, z)` | 绝对定位 |
| `translate(doc, name, dx, dy, dz)` | 相对平移 |
| `rotate_obj(doc, name, axis, angle, center?)` | 旋转 |
| `mirror_obj(doc, name, plane, offset?)` | 镜像 |
| `scale_obj(doc, name, factor)` | 缩放 |
| `copy_obj(doc, name, new_name)` | 复制 |
| `delete_obj(doc, name)` | 删除 |
| `hide_obj(doc, name)` / `show_obj(doc, name)` | 显示/隐藏 |
| `set_placement(doc, name, position?, rotation?)` | 设置 Placement |

### 阵列

| 函数 | 说明 |
|------|------|
| `circular_array(doc, template, count, radius, start_angle?)` | 圆周阵列 |
| `linear_array(doc, template, count, step, direction?)` | 直线阵列 |

### 导出

| 函数 | 说明 |
|------|------|
| `export_step(doc, obj_names?, out_path?)` | 导出 STEP |
| `export_stl(doc, obj_names?, out_path?)` | 导出 STL |

### 诊断

| 函数 | 说明 |
|------|------|
| `list_all(doc?)` | 列出所有对象 |
| `inspect_obj(doc, name)` | 详细检查单个对象 |
| `get_volume(doc, name)` | 体积 mm³ |
| `get_mass(doc, name, density?)` | 质量 kg |
| `check_exists(doc, name)` | 存在性检查 |
| `get_bounds(doc, name)` | 包围盒 |
| `classify_edges(doc, name)` | 边分类 |

## 密度参考 (kg/mm³)

| 材料 | 密度 |
|------|------|
| 45 钢 | 7.85e-6 |
| 6061 铝 | 2.70e-6 |
| ABS | 1.04e-6 |
| 铜 | 8.96e-6 |

## 典型工作流

### 零件建模（Part 工作流）

```python
exec(open('G:/Program/go/seelex/plugins/freecad/cad-core/freecad_core.py').read())

doc = new_doc("PartDesign")

# 1. 创建体素
base = make_box(doc, "Base", 50, 50, 50)
hole = make_cylinder(doc, "Hole", 10, 60, center=(25, 25, 25))

# 2. 布尔运算
result = bool_cut(doc, "Result", "Base", "Hole")

# 3. 验证
inspect_obj(doc, "Result")

# 4. 导出
export_step(doc, out_path="~/Desktop")
```

### 装配件（多体融合）

```python
# 创建多个零件
plate = make_box(doc, "Plate", 100, 80, 10)
boss = make_cylinder(doc, "Boss", 15, 20, center=(50, 40, 15))
rib = make_box(doc, "Rib", 6, 30, 15, center=(50, 20, 15))

# 融合为装配体
assembly = multi_fuse(doc, "Assembly", ["Plate", "Boss", "Rib"])
```

## 设计原则

1. **体素 + 布尔 = 任意零件** — Part 工作流比 PartDesign Body/Sketch 稳定
2. **不分零件/装配件特化** — 所有操作使用相同的体素+布尔+变换基础
3. **先创建后合并** — 所有体素创建完再 recompute，避免中间状态
4. **每步验证** — 操作后用 `inspect_obj` 确认结果
