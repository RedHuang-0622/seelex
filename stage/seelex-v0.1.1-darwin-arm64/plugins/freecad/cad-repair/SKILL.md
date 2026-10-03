---
description: 修复与恢复 — 系统异常时诊断→清理→恢复→验证
---

# 修复与恢复

操作中断、对象损坏或系统处于异常状态时，按标准流程恢复。全部操作在 FreeCAD Python 控制台中执行。

依赖 `cad-core/freecad_core.py` 的 `list_all()`, `inspect_obj()`, `recompute()`, `undo()`, `delete_obj()`。

## 标准修复流程

```python
exec(open('G:/Program/go/seelex/plugins/freecad/cad-core/freecad_core.py').read())

doc = get_doc()

# 1. 诊断：列出所有对象
list_all()

# 2. 识别问题对象
# 检查无效 Shape
for obj in doc.Objects:
    if hasattr(obj, 'Shape') and obj.Shape and not obj.Shape.isValid():
        print(f"INVALID: {obj.Name}")

# 3. 清理：移除失败对象
to_remove = ["BadFillet", "FailedCut"]  # 根据诊断结果修改
for name in to_remove:
    try:
        delete_obj(doc, name)
    except:
        pass

# 4. 恢复
recompute()

# 5. 验证
list_all()
```

## 常见恢复场景

| 场景 | 操作 |
|------|------|
| 布尔操作后对象消失 | `list_all()` 确认当前对象名（Cut/Fuse 结果自动编号） |
| Fillet 计算失败 | 清理旧 Fillet → 用 cad-fillet 分批重新倒角 |
| FreeCAD 卡死 | 保存 → 重启 FreeCAD |
| 依赖链锁定 | `undo()` 回退 → 修正 → 重建 |

## 脚本

| 脚本 | 用途 |
|------|------|
| `fix.py` | 通用修复脚本（诊断 + 查找基准对象） |
