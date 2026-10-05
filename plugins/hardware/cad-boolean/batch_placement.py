# batch_placement.py — 批量创建对象副本并放置到指定位置
# 适用场景：螺栓孔阵列、沉头孔阵列、加强筋阵列等
# 用法: 在 FreeCAD Python 控制台中粘贴运行

import FreeCAD as App
import Part
import math


def circular_array(doc, template_obj, count, radius, start_angle=0, name_prefix="Copy"):
    """
    将模板对象沿圆周阵列复制。

    参数:
        doc: FreeCAD 文档对象
        template_obj: 要复制的模板对象（Part::Feature）
        count: 副本数量
        radius: 阵列半径 (PCD/2)
        start_angle: 起始角度（度）
        name_prefix: 副本名称前缀

    返回:
        副本对象列表
    """
    copies = []
    for i in range(count):
        angle = math.radians(start_angle + i * (360.0 / count))
        x = radius * math.cos(angle)
        y = radius * math.sin(angle)

        c = doc.addObject(template_obj.TypeId, f"{name_prefix}_{i+1}")
        # 复制属性（根据类型）
        for prop in template_obj.PropertiesList:
            if prop not in ("Placement", "Label"):
                try:
                    setattr(c, prop, getattr(template_obj, prop))
                except:
                    pass
        c.Label = f"{name_prefix} #{i+1}"
        # 设置位置：保持原 Z 坐标
        old_z = template_obj.Placement.Base.z
        c.Placement.Base = App.Vector(x, y, old_z)
        copies.append(c)

    print(f"✅ circular_array: {count} 个副本 @ R{radius}mm")
    return copies


def linear_array(doc, template_obj, count, step, direction=(1, 0, 0), name_prefix="Copy"):
    """
    将模板对象沿直线阵列复制。

    参数:
        doc: FreeCAD 文档对象
        template_obj: 模板对象
        count: 副本数量（不含模板）
        step: 步长 mm
        direction: 方向向量 (dx, dy, dz)
        name_prefix: 副本名称前缀

    返回:
        副本对象列表
    """
    dx, dy, dz = direction
    norm = math.sqrt(dx*dx + dy*dy + dz*dz)
    dx, dy, dz = dx/norm * step, dy/norm * step, dz/norm * step

    copies = []
    for i in range(1, count + 1):
        c = doc.addObject(template_obj.TypeId, f"{name_prefix}_{i}")
        for prop in template_obj.PropertiesList:
            if prop not in ("Placement", "Label"):
                try:
                    setattr(c, prop, getattr(template_obj, prop))
                except:
                    pass
        c.Label = f"{name_prefix} #{i}"
        old = template_obj.Placement.Base
        c.Placement.Base = App.Vector(old.x + dx*i, old.y + dy*i, old.z + dz*i)
        copies.append(c)

    print(f"✅ linear_array: {count} 个副本 @ {step}mm")
    return copies


def mirror_copy(doc, obj, plane="XY", offset=0, name_prefix="Mirror"):
    """
    创建对象的镜像副本（通过 Placement 变换实现）。

    参数:
        doc: FreeCAD 文档对象
        obj: 源对象
        plane: 镜像平面 ("XY" / "XZ" / "YZ")
        offset: 平面偏移
        name_prefix: 副本名称前缀

    返回:
        镜像对象
    """
    m = doc.addObject(obj.TypeId, f"{name_prefix}_{obj.Name}")
    for prop in obj.PropertiesList:
        if prop not in ("Placement", "Label"):
            try:
                setattr(m, prop, getattr(obj, prop))
            except:
                pass
    m.Label = f"{name_prefix} {obj.Label}"

    old = obj.Placement.Base
    if plane == "XY":
        m.Placement.Base = App.Vector(old.x, old.y, -old.z + 2*offset)
    elif plane == "XZ":
        m.Placement.Base = App.Vector(old.x, -old.y + 2*offset, old.z)
    elif plane == "YZ":
        m.Placement.Base = App.Vector(-old.x + 2*offset, old.y, old.z)

    doc.recompute()
    print(f"✅ mirror_copy: {plane} plane @ offset={offset}")
    return m


# ============ 使用示例 ============
if __name__ == "__main__":
    doc = App.ActiveDocument
    if doc is None:
        doc = App.newDocument("PlacementTest")

    for obj in doc.Objects:
        doc.removeObject(obj.Name)

    # 创建模板：螺栓孔圆柱
    template = doc.addObject("Part::Cylinder", "Template")
    template.Radius = 6
    template.Height = 20
    template.Placement.Base = App.Vector(0, 0, -10)

    # 圆周阵列 6 个螺栓孔
    holes = circular_array(doc, template, count=6, radius=45, start_angle=30, name_prefix="BoltHole")
    doc.recompute()

    # 创建模板：肋板（方盒）
    rib = doc.addObject("Part::Box", "RibTemplate")
    rib.Length = 6
    rib.Width = 14
    rib.Height = 16
    rib.Placement.Base = App.Vector(-3, 20, -8)

    # 圆周阵列 6 个肋板
    ribs = circular_array(doc, rib, count=6, radius=0, start_angle=0, name_prefix="Rib")

    print("🎉 完成！")
