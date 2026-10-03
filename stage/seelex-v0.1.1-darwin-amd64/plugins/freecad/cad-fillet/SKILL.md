---
description: 分批圆角 — 分批倒角策略，避免 Part Fillet 在边数过多时计算失败
---

# 分批圆角处理

FreeCAD Part Fillet 在边数过多时计算失败。采用**分批倒角**策略，直接在 Python 控制台执行。

## 策略

1. 用 `classify_edges()` (cad-core) 或 `obj.Shape.Edges` 获取所有边
2. 按边长度筛选（跳过短边避免冲突）
3. 每批处理 5 条边，生成一个 Fillet 对象
4. 链式传递：`F_0` → `F_1` → `F_2` ...

## 使用方式

### 方式一：cad-core + 手动分批

```python
exec(open('G:/Program/go/seelex/plugins/freecad/cad-core/freecad_core.py').read())

doc = get_doc()
obj = doc.getObject("TargetObject")

# 分类边
ext_edges, int_edges, _ = classify_edges(doc, "TargetObject")

# 分批圆角 (每批 5 条边)
batch_size = 5
current_obj = obj
for batch_start in range(0, len(ext_edges), batch_size):
    batch = ext_edges[batch_start:batch_start+batch_size]
    name = f'F_{batch_start//batch_size}'
    fillet = doc.addObject('Part::Fillet', name)
    fillet.Base = current_obj
    fillet.Edges = [(idx, 2.0, 2.0) for idx, _, _ in batch]
    doc.recompute()
    if fillet.Shape and fillet.Shape.isValid():
        current_obj = fillet
        print(f'  Batch {batch_start//batch_size}: OK')
    else:
        doc.removeObject(name)
        print(f'  Batch {batch_start//batch_size}: FAIL')

current_obj.Label = 'FinalFilleted'
```

### 方式二：现成脚本

| 脚本 | 用途 | 策略 |
|------|------|------|
| `batch_fillet_clean.py` | 分批倒角（推荐） | 每批 5 条边，链式传递 |
| `batch_fillet.py` | 分批倒角 | 每批 20 条边 |
| `auto_fillet.py` | 自动倒角 | 按长度筛选外部边 |
| `selective_fillet.py` | 选择性倒角 | 按距离条件筛选 |
| `brute_fillet.py` | 暴力尝试 | 所有边同时圆角 |
| `onebyone.py` | 逐个测试 | 单边调试用 |
| `single_fillet.py` | 单次圆角 | 外部/内部分别圆角 |

## 手动步骤

1. 打开 FreeCAD → View → Panels → Python console
2. 加载 cad-core: `exec(open('G:/Program/go/seelex/plugins/freecad/cad-core/freecad_core.py').read())`
3. 用 `classify_edges()` 分析边
4. 修改目标对象名
5. 粘贴分批圆角代码执行
