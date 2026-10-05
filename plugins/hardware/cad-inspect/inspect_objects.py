# inspect_objects.py
# 用法: 在 FreeCAD Python 控制台运行此脚本，列出所有对象及其属性
# 诊断用 — 查看文档中有什么对象

import FreeCAD as App
import FreeCADGui as Gui

def inspect_objects():
    doc = App.ActiveDocument
    if doc is None:
        print("❌ 没有打开的活动文档")
        return

    print(f"\n📄 文档: {doc.Name} (Label: {doc.Label})")
    print(f"   对象总数: {len(doc.Objects)}")
    print("-" * 70)

    for obj in doc.Objects:
        name = obj.Name
        label = obj.Label
        type_name = obj.TypeId
        vis = "👁" if obj.ViewObject and obj.ViewObject.Visibility else "🚫"
        # 尝试获取形状信息
        shape_info = ""
        if hasattr(obj, "Shape") and obj.Shape:
            try:
                s = obj.Shape
                vol = s.Volume
                area = s.Area
                shape_info = f"  V={vol:.1f}mm³  A={area:.1f}mm²"
            except:
                shape_info = "  (形状可用)"
        print(f"  {vis} {name:30s} | {label:30s} | {type_name:40s}{shape_info}")

    print("-" * 70)
    print(f"✅ 共 {len(doc.Objects)} 个对象\n")

if __name__ == "__main__":
    inspect_objects()
