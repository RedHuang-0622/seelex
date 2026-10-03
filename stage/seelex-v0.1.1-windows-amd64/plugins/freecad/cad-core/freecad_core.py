# freecad_core.py — FreeCAD 核心操作库
# 替代 MCP：所有基础体素构造 + 布尔运算 + 变换 + 导出
# 用法: 在 FreeCAD Python 控制台中粘贴运行
#       exec(open('G:/Program/go/seelex/plugins/freecad/cad-core/freecad_core.py').read())
#       然后调用函数: make_box(doc, 'MyBox', 100, 50, 20)

import FreeCAD as App
import Part
import math
import os

# ============================================================
# 文档操作
# ============================================================

def new_doc(name="Unnamed"):
    """创建新文档"""
    doc = App.newDocument(name)
    print(f"📄 新文档: {doc.Name}")
    return doc

def get_doc(name=None):
    """获取文档: name 指定则 getDocument，否则 ActiveDocument"""
    if name:
        return App.getDocument(name)
    doc = App.ActiveDocument
    if doc is None:
        raise RuntimeError("没有活动文档，请先 new_doc() 或打开文档")
    return doc

def save_doc(doc=None, path=None):
    """保存文档"""
    if doc is None:
        doc = get_doc()
    if path:
        doc.saveAs(path)
    else:
        doc.save()
    print(f"💾 已保存: {doc.FileName or doc.Name}")

def recompute(doc=None):
    """重新计算文档"""
    if doc is None:
        doc = get_doc()
    doc.recompute()
    print("🔄 recompute 完成")

def undo(doc=None):
    """撤销"""
    if doc is None:
        doc = get_doc()
    doc.undo()
    print("↩ undo")

def redo(doc=None):
    """重做"""
    if doc is None:
        doc = get_doc()
    doc.redo()
    print("↪ redo")

def clear_all(doc=None):
    """删除文档中所有对象"""
    if doc is None:
        doc = get_doc()
    for obj in list(doc.Objects):
        try:
            doc.removeObject(obj.Name)
        except:
            pass
    print("🧹 已清空文档")


# ============================================================
# 体素构造 (Part 工作台)
# 所有 make_* 函数返回创建的对象
# center 参数为几何中心 (x, y, z)，None 表示原点
# ============================================================

def make_box(doc, name, length, width, height, center=None):
    """创建 Box。center 为几何中心，默认原点"""
    obj = doc.addObject("Part::Box", name)
    obj.Length = length
    obj.Width = width
    obj.Height = height
    if center is not None:
        obj.Placement.Base = App.Vector(
            center[0] - length / 2,
            center[1] - width / 2,
            center[2] - height / 2,
        )
    print(f"📦 {name}: Box {length}×{width}×{height} mm")
    return obj

def make_cylinder(doc, name, radius, height, center=None):
    """创建圆柱体。center 为几何中心"""
    obj = doc.addObject("Part::Cylinder", name)
    obj.Radius = radius
    obj.Height = height
    if center is not None:
        obj.Placement.Base = App.Vector(
            center[0], center[1], center[2] - height / 2
        )
    print(f"🔵 {name}: Cylinder R{radius} H{height} mm")
    return obj

def make_sphere(doc, name, radius, center=None):
    """创建球体"""
    obj = doc.addObject("Part::Sphere", name)
    obj.Radius = radius
    if center is not None:
        obj.Placement.Base = App.Vector(*center)
    print(f"🔮 {name}: Sphere R{radius} mm")
    return obj

def make_cone(doc, name, radius1, radius2, height, center=None):
    """创建圆锥/圆台。radius2=0 为圆锥"""
    obj = doc.addObject("Part::Cone", name)
    obj.Radius1 = radius1
    obj.Radius2 = radius2
    obj.Height = height
    if center is not None:
        obj.Placement.Base = App.Vector(
            center[0], center[1], center[2] - height / 2
        )
    print(f"🔺 {name}: Cone R1={radius1} R2={radius2} H={height} mm")
    return obj

def make_torus(doc, name, radius1, radius2, center=None):
    """创建圆环体。radius1=环半径, radius2=管半径"""
    obj = doc.addObject("Part::Torus", name)
    obj.Radius1 = radius1
    obj.Radius2 = radius2
    if center is not None:
        obj.Placement.Base = App.Vector(*center)
    print(f"🍩 {name}: Torus R1={radius1} R2={radius2} mm")
    return obj

def make_wedge(doc, name, xmin, ymin, zmin, x2min, z2min, xmax, ymax, zmax, x2max, z2max):
    """创建楔形体 (Part::Wedge 需要完整的 10 参数)"""
    obj = doc.addObject("Part::Wedge", name)
    obj.Xmin = xmin
    obj.Ymin = ymin
    obj.Zmin = zmin
    obj.X2min = x2min
    obj.Z2min = z2min
    obj.Xmax = xmax
    obj.Ymax = ymax
    obj.Zmax = zmax
    obj.X2max = x2max
    obj.Z2max = z2max
    print(f"🔺 {name}: Wedge")
    return obj

def make_helix(doc, name, pitch, height, radius, center=None, angle=0):
    """创建螺旋线 (Part::Helix)，用于螺纹扫掠路径"""
    obj = doc.addObject("Part::Helix", name)
    obj.Pitch = pitch
    obj.Height = height
    obj.Radius = radius
    obj.Angle = angle
    if center is not None:
        obj.Placement.Base = App.Vector(*center)
    print(f"🌀 {name}: Helix pitch={pitch} H={height} R={radius} mm")
    return obj

def make_regular_polygon(doc, name, sides, radius, center=None):
    """
    创建正多边形线框 (Part::RegularPolygon)。
    用于六角头螺栓头部等。
    """
    obj = doc.addObject("Part::RegularPolygon", name)
    obj.Polygon = sides
    obj.Circumradius = radius
    if center is not None:
        obj.Placement.Base = App.Vector(*center)
    print(f"⬡ {name}: RegularPolygon {sides} sides R={radius} mm")
    return obj

def make_feature(doc, name, shape=None):
    """
    创建通用 Part::Feature（用于封装 TopoShape）。
    典型用途：将 Part.TopoShape 封装为文档对象，如螺纹截面。
    """
    obj = doc.addObject("Part::Feature", name)
    if shape is not None:
        obj.Shape = shape
    print(f"🔧 {name}: Feature")
    return obj


# ============================================================
# 布尔运算
# ============================================================

def bool_cut(doc, name, base_name, tool_name):
    """单次布尔剪切: base - tool"""
    obj = doc.addObject("Part::Cut", name)
    obj.Base = doc.getObject(base_name)
    obj.Tool = doc.getObject(tool_name)
    doc.recompute()
    print(f"✂️ {name}: {base_name} - {tool_name}")
    return obj

def bool_fuse(doc, name, obj1_name, obj2_name):
    """单次布尔融合: obj1 + obj2"""
    obj = doc.addObject("Part::Fuse", name)
    obj.Base = doc.getObject(obj1_name)
    obj.Tool = doc.getObject(obj2_name)
    doc.recompute()
    print(f"🔗 {name}: {obj1_name} + {obj2_name}")
    return obj

def bool_common(doc, name, obj1_name, obj2_name):
    """布尔交集: obj1 ∩ obj2"""
    obj = doc.addObject("Part::Common", name)
    obj.Base = doc.getObject(obj1_name)
    obj.Tool = doc.getObject(obj2_name)
    doc.recompute()
    print(f"∩ {name}: {obj1_name} ∩ {obj2_name}")
    return obj

def multi_fuse(doc, name, obj_names):
    """
    多对象融合：将多个对象合并为一个。
    先创建 MultiFuse 然后 refine 为单一实体。
    """
    objs = [doc.getObject(n) for n in obj_names]
    fuse = doc.addObject("Part::MultiFuse", name + "_MultiFuse")
    fuse.Shapes = objs
    doc.recompute()
    # 尝试 refine 为单一 shape
    try:
        refined_shape = fuse.Shape.removeSplitter()
        result = doc.addObject("Part::Feature", name)
        result.Shape = refined_shape
        doc.removeObject(fuse.Name)
        doc.recompute()
        print(f"🔗 {name}: {len(objs)} objects fused → refined")
        return result
    except:
        print(f"🔗 {name}: {len(objs)} objects fused (MultiFuse)")
        return fuse

def multi_cut(doc, base_name, tool_defs, cut_name="Cut_Result"):
    """
    多工具一次性布尔剪切 — 只触发一次 recompute。

    tool_defs 列表，每项 dict:
        {"type": "cylinder", "name": "Tool_1", "radius": 6, "height": 20, "center": (x,y,z)}
        支持 type: cylinder / box / sphere / cone
    """
    tools = []
    for td in tool_defs:
        t = td["type"]
        name = td["name"]
        center = td.get("center", (0, 0, 0))
        if t == "cylinder":
            obj = doc.addObject("Part::Cylinder", name)
            obj.Radius = td["radius"]
            obj.Height = td["height"]
            obj.Placement.Base = App.Vector(center[0], center[1], center[2] - td["height"] / 2)
        elif t == "box":
            obj = doc.addObject("Part::Box", name)
            obj.Length = td["length"]
            obj.Width = td["width"]
            obj.Height = td["height"]
            obj.Placement.Base = App.Vector(center[0] - td["length"]/2, center[1] - td["width"]/2, center[2] - td["height"]/2)
        elif t == "sphere":
            obj = doc.addObject("Part::Sphere", name)
            obj.Radius = td["radius"]
            obj.Placement.Base = App.Vector(*center)
        elif t == "cone":
            obj = doc.addObject("Part::Cone", name)
            obj.Radius1 = td["radius1"]
            obj.Radius2 = td.get("radius2", 0)
            obj.Height = td["height"]
            obj.Placement.Base = App.Vector(center[0], center[1], center[2] - td["height"]/2)
        else:
            raise ValueError(f"不支持的工具类型: {t}")
        tools.append(obj)

    if not tools:
        print("⚠️ 没有工具对象")
        return doc.getObject(base_name)

    fuse = doc.addObject("Part::MultiFuse", "Fuse_Tools")
    fuse.Shapes = tools
    cut = doc.addObject("Part::Cut", cut_name)
    cut.Base = doc.getObject(base_name)
    cut.Tool = fuse
    doc.recompute()
    print(f"✂️ {cut_name}: {base_name} - {len(tools)} tools")
    return cut


# ============================================================
# 变换 (Placement / 移动 / 旋转 / 缩放)
# ============================================================

def move_to(doc, obj_name, x, y, z):
    """绝对定位 — 将对象中心移动到指定坐标"""
    obj = doc.getObject(obj_name)
    obj.Placement.Base = App.Vector(x, y, z)
    print(f"📍 {obj_name} → ({x}, {y}, {z})")

def translate(doc, obj_name, dx, dy, dz):
    """相对平移"""
    obj = doc.getObject(obj_name)
    b = obj.Placement.Base
    obj.Placement.Base = App.Vector(b.x + dx, b.y + dy, b.z + dz)
    print(f"↗ {obj_name} +({dx}, {dy}, {dz})")

def rotate_obj(doc, obj_name, axis, angle, center=None):
    """
    旋转对象。
    axis: 'X' | 'Y' | 'Z' 或 (x, y, z) 向量
    angle: 角度（度）
    center: 旋转中心 (x, y, z)，默认原点
    """
    obj = doc.getObject(obj_name)
    if isinstance(axis, str):
        axis_map = {'X': (1, 0, 0), 'Y': (0, 1, 0), 'Z': (0, 0, 1)}
        if axis.upper() not in axis_map:
            raise ValueError(f"axis 必须是 X/Y/Z 或 (x,y,z)，得到: {axis}")
        axis = axis_map[axis.upper()]
    rotation = App.Rotation(App.Vector(*axis), angle)
    if center is not None:
        # 绕指定中心旋转——需要组合平移
        b = obj.Placement.Base
        cx, cy, cz = center
        # 平移到原点
        obj.Placement.Base = App.Vector(b.x - cx, b.y - cy, b.z - cz)
        obj.Placement.Rotation = rotation.multiply(obj.Placement.Rotation)
        obj.Placement.Base = App.Vector(
            obj.Placement.Base.x + cx,
            obj.Placement.Base.y + cy,
            obj.Placement.Base.z + cz,
        )
    else:
        obj.Placement.Rotation = rotation.multiply(obj.Placement.Rotation)
    print(f"🔄 {obj_name}: rotate {angle}° around {axis}")

def mirror_obj(doc, obj_name, plane="XY", offset=0):
    """
    创建镜像副本。
    plane: "XY" | "XZ" | "YZ"
    """
    obj = doc.getObject(obj_name)
    mirror = doc.addObject(obj.TypeId, f"Mirror_{obj_name}")
    for prop in obj.PropertiesList:
        if prop not in ("Placement", "Label"):
            try:
                setattr(mirror, prop, getattr(obj, prop))
            except:
                pass
    mirror.Label = f"Mirror {obj.Label}"
    old = obj.Placement.Base
    if plane == "XY":
        mirror.Placement.Base = App.Vector(old.x, old.y, -old.z + 2 * offset)
    elif plane == "XZ":
        mirror.Placement.Base = App.Vector(old.x, -old.y + 2 * offset, old.z)
    elif plane == "YZ":
        mirror.Placement.Base = App.Vector(-old.x + 2 * offset, old.y, old.z)
    else:
        raise ValueError(f"plane 必须是 XY/XZ/YZ，得到: {plane}")
    doc.recompute()
    print(f"🪞 {mirror.Name}: mirror across {plane} plane")
    return mirror

def scale_obj(doc, obj_name, factor, name=None):
    """均匀缩放对象（创建缩放副本）"""
    obj = doc.getObject(obj_name)
    if name is None:
        name = f"Scale_{obj_name}"
    s = doc.addObject("Part::Feature", name)
    s.Shape = obj.Shape.copy().scale(factor)
    doc.recompute()
    print(f"📐 {name}: scaled ×{factor}")
    return s

def copy_obj(doc, obj_name, new_name):
    """复制对象（完整副本）"""
    obj = doc.getObject(obj_name)
    c = doc.addObject(obj.TypeId, new_name)
    for prop in obj.PropertiesList:
        if prop not in ("Label",):
            try:
                setattr(c, prop, getattr(obj, prop))
            except:
                pass
    c.Label = new_name
    print(f"📋 {new_name}: copy of {obj_name}")
    return c

def delete_obj(doc, obj_name):
    """删除对象"""
    doc.removeObject(obj_name)
    print(f"🗑 {obj_name} 已删除")

def hide_obj(doc, obj_name):
    """隐藏对象"""
    obj = doc.getObject(obj_name)
    if obj.ViewObject:
        obj.ViewObject.Visibility = False
    print(f"🙈 {obj_name} 隐藏")

def show_obj(doc, obj_name):
    """显示对象"""
    obj = doc.getObject(obj_name)
    if obj.ViewObject:
        obj.ViewObject.Visibility = True
    print(f"👁 {obj_name} 显示")

def set_placement(doc, obj_name, position=None, rotation=None):
    """
    设置对象 Placement。
    position: (x, y, z)
    rotation: 可以是 (axis_vector, angle) 元组，如 ((0,0,1), 45)
    """
    obj = doc.getObject(obj_name)
    if position is not None:
        obj.Placement.Base = App.Vector(*position)
    if rotation is not None:
        axis, angle = rotation
        obj.Placement.Rotation = App.Rotation(App.Vector(*axis), angle)
    print(f"📌 {obj_name}: placement set")


# ============================================================
# 导出
# ============================================================

def export_step(doc, obj_names=None, out_path=None):
    """
    导出 STEP 文件。
    obj_names: 对象名列表，None=全部可见有 Shape 的对象
    out_path: 输出路径，None=桌面
    """
    return _export(doc, obj_names, out_path, "step")

def export_stl(doc, obj_names=None, out_path=None):
    """导出 STL 文件"""
    return _export(doc, obj_names, out_path, "stl")

def _export(doc, obj_names, out_path, fmt):
    if obj_names is None:
        objects = [o for o in doc.Objects if hasattr(o, "Shape") and o.Shape and o.ViewObject and o.ViewObject.Visibility]
    else:
        objects = [doc.getObject(n) for n in obj_names]

    if not objects:
        print("❌ 没有可导出的对象")
        return []

    if out_path is None:
        out_path = os.path.expanduser("~/Desktop")
    os.makedirs(out_path, exist_ok=True)

    exported = []
    for obj in objects:
        safe_name = "".join(c if c.isalnum() or c in "._-" else "_" for c in obj.Label)
        fname = f"{safe_name}.{fmt}" if fmt == "step" else f"{safe_name}.stl"
        fpath = os.path.join(out_path, fname)
        try:
            part_objs = [obj]
            Part.export(part_objs, fpath)
            exported.append(fpath)
            print(f"📦 {fpath}")
        except Exception as e:
            print(f"❌ 导出 {obj.Label} 失败: {e}")

    print(f"✅ 导出 {len(exported)} 个 .{fmt} 文件 → {out_path}")
    return exported


# ============================================================
# 阵列 (圆周 / 直线)
# ============================================================

def circular_array(doc, template_obj, count, radius, start_angle=0, name_prefix="Copy"):
    """
    圆周阵列复制模板对象。

    参数:
        template_obj: 模板对象（doc.addObject 返回值或名称字符串）
        count: 副本数量
        radius: 阵列半径 (PCD/2)
        start_angle: 起始角度（度）
    """
    if isinstance(template_obj, str):
        template_obj = doc.getObject(template_obj)
    copies = []
    for i in range(count):
        angle = math.radians(start_angle + i * (360.0 / count))
        x = radius * math.cos(angle)
        y = radius * math.sin(angle)
        c = doc.addObject(template_obj.TypeId, f"{name_prefix}_{i+1}")
        for prop in template_obj.PropertiesList:
            if prop not in ("Placement", "Label"):
                try:
                    setattr(c, prop, getattr(template_obj, prop))
                except:
                    pass
        c.Label = f"{name_prefix} #{i+1}"
        old_z = template_obj.Placement.Base.z
        c.Placement.Base = App.Vector(x, y, old_z)
        copies.append(c)
    print(f"🔵 circular_array: {count} 副本 @ R{radius}mm")
    return copies

def linear_array(doc, template_obj, count, step, direction=(1, 0, 0), name_prefix="Copy"):
    """
    直线阵列复制模板对象。

    参数:
        template_obj: 模板对象（对象或名称字符串）
        count: 副本数量（不含模板）
        step: 步长 mm
        direction: 方向向量 (dx, dy, dz)
    """
    if isinstance(template_obj, str):
        template_obj = doc.getObject(template_obj)
    dx, dy, dz = direction
    norm = math.sqrt(dx*dx + dy*dy + dz*dz)
    dx = dx / norm * step
    dy = dy / norm * step
    dz = dz / norm * step

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
    print(f"↔ linear_array: {count} 副本 @ {step}mm")
    return copies


# ============================================================
# 检查与诊断
# ============================================================

def list_all(doc=None):
    """列出文档中所有对象"""
    if doc is None:
        doc = get_doc()
    print(f"\n📄 {doc.Name} ({len(doc.Objects)} objects)")
    print("-" * 70)
    for obj in doc.Objects:
        vis = "👁" if (obj.ViewObject and obj.ViewObject.Visibility) else "🚫"
        shape_info = ""
        if hasattr(obj, "Shape") and obj.Shape:
            try:
                shape_info = f"  V={obj.Shape.Volume:.1f}mm³"
            except:
                shape_info = "  (shape)"
        print(f"  {vis} {obj.Name:30s} | {obj.Label:30s} | {obj.TypeId:40s}{shape_info}")
    print("-" * 70)
    return doc.Objects

def inspect_obj(doc, obj_name):
    """检查单个对象的详细信息"""
    obj = doc.getObject(obj_name)
    if obj is None:
        print(f"❌ {obj_name} 不存在")
        return None
    print(f"\n🔍 {obj.Name}")
    print(f"   Type:    {obj.TypeId}")
    print(f"   Label:   {obj.Label}")
    if hasattr(obj, "Shape") and obj.Shape:
        s = obj.Shape
        try:
            print(f"   Volume:  {s.Volume:.2f} mm³")
            print(f"   Area:    {s.Area:.2f} mm²")
            print(f"   CoM:     ({s.CenterOfMass.x:.2f}, {s.CenterOfMass.y:.2f}, {s.CenterOfMass.z:.2f})")
            print(f"   Valid:   {s.isValid()}")
        except Exception as e:
            print(f"   Shape:   (error: {e})")
    if obj.Placement:
        b = obj.Placement.Base
        print(f"   Position: ({b.x:.2f}, {b.y:.2f}, {b.z:.2f})")
    return obj

def get_volume(doc, obj_name):
    """获取对象体积 (mm³)"""
    obj = doc.getObject(obj_name)
    if obj and hasattr(obj, "Shape") and obj.Shape:
        return obj.Shape.Volume
    return 0

def get_mass(doc, obj_name, density=7.85e-6):
    """获取质量 (kg)。density 默认 7.85e-6 kg/mm³ (45钢)"""
    vol = get_volume(doc, obj_name)
    mass = vol * density
    print(f"⚖ {obj_name}: V={vol:.1f} mm³, mass={mass:.3f} kg (ρ={density})")
    return mass

def check_exists(doc, obj_name):
    """检查对象是否存在。返回 bool"""
    exists = doc.getObject(obj_name) is not None
    if not exists:
        print(f"❌ {obj_name} 不存在")
    return exists

def get_bounds(doc, obj_name):
    """获取对象包围盒"""
    obj = doc.getObject(obj_name)
    if obj and hasattr(obj, "Shape") and obj.Shape:
        bb = obj.Shape.BoundBox
        print(f"📐 {obj_name}: BBox ({bb.XMin:.1f},{bb.YMin:.1f},{bb.ZMin:.1f}) → ({bb.XMax:.1f},{bb.YMax:.1f},{bb.ZMax:.1f})")
        print(f"   Size: {bb.XLength:.1f} × {bb.YLength:.1f} × {bb.ZLength:.1f} mm")
        return bb
    return None


# ============================================================
# 分类边 (用于圆角)
# ============================================================

def classify_edges(doc, obj_name):
    """按位置和类型分类边，返回分类结果"""
    obj = doc.getObject(obj_name)
    edges = obj.Shape.Edges
    ext_edges, int_edges, other = [], [], []
    for i, e in enumerate(edges):
        try:
            cog = e.CenterOfGravity
            r = math.sqrt(cog.x**2 + cog.y**2)
            length = e.Length
            etype = type(e.Curve).__name__
            if r > 50 and length > 3:
                ext_edges.append((i + 1, length, etype))
            elif length > 3:
                int_edges.append((i + 1, length, etype))
            else:
                other.append((i + 1, length, etype))
        except:
            other.append((i + 1, 0, "unknown"))
    print(f"🔍 {obj_name}: {len(ext_edges)} ext + {len(int_edges)} int + {len(other)} other = {len(edges)} total")
    return ext_edges, int_edges, other


# ============================================================
# 打印提示
# ============================================================

print("✅ freecad_core.py 已加载")
print("   可用函数: new_doc, get_doc, save_doc, recompute, undo, redo, clear_all")
print("   体素: make_box, make_cylinder, make_sphere, make_cone, make_torus, make_wedge, make_helix, make_regular_polygon, make_feature")
print("   布尔: bool_cut, bool_fuse, bool_common, multi_fuse, multi_cut")
print("   变换: move_to, translate, rotate_obj, mirror_obj, scale_obj, copy_obj, delete_obj, hide_obj, show_obj, set_placement")
print("   阵列: circular_array, linear_array")
print("   导出: export_step, export_stl")
print("   诊断: list_all, inspect_obj, get_volume, get_mass, check_exists, get_bounds, classify_edges")
