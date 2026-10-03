---
description: 参数化模板 — 定义参数 → 自动化执行 → 输出，适用于重复性设计和标准化流程
---

# 参数化模板设计

依赖 `cad-core/freecad_core.py` 的体素 + 布尔 + 导出函数。

集中参数定义 + 分步执行 + 结果验证。适用于法兰、齿轮、支架等标准件按参数生成。

## 模板结构

```python
exec(open('G:/Program/go/seelex/plugins/freecad/cad-core/freecad_core.py').read())

doc = new_doc("TemplateName")

# ========== Parameters ==========
OD, ID, THK = 120, 50, 20    # mm
PCD, BOLT_D = 90, 12
MATERIAL_DENSITY = 7.85e-6    # kg/mm³ (45 steel)

# ========== Step 1: Body ==========
outer = make_cylinder(doc, "Outer", OD/2, THK)
inner = make_cylinder(doc, "Inner", ID/2, THK+2, center=(0,0,THK/2))
ring = bool_cut(doc, "Ring", "Outer", "Inner")

# ========== Step 2: Features ==========
# circular array of bolt holes, keyway, ribs...

# ========== Validation ==========
inspect_obj(doc, "FinalObject")
get_mass(doc, "FinalObject", MATERIAL_DENSITY)

# ========== Export ==========
export_step(doc, out_path="~/Desktop")
```

## 原则

- 参数集中在文件顶部，不要散落在代码中
- 每个参数注明单位、默认值、取值范围
- 步骤之间用分隔线隔开，方便 LLM 分步理解
- 执行后验证参数是否正确应用
- 体素 + 布尔构建所有特征，不区分零件/装配件

## 脚本

| 脚本 | 用途 |
|------|------|
| `gen_flange.py` | 法兰联轴器参数化生成（完整示例，131行） |
| `gen_flange2.py` | 法兰变体 |
| `batch_export.py` | 批量 STEP/STL 导出工具 |

## 示例输出

`FlangeCoupling.step` — 参考输出文件
