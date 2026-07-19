# batch_boolean_cut.py — 多工具一次性布尔剪切
# 避免逐条布尔导致 recompute 堆积超时
# 用法: 在 FreeCAD Python 控制台中粘贴运行
#       或加载 cad-core 后直接调用 multi_cut()
#       自定义 main() 中的 base_name、tool_defs 即可

import FreeCAD as App
import Part


def multi_cut(doc, base_name, tool_defs, cut_name="Cut_Result"):
    """
    一次性创建多个工具并剪切，仅触发一次 recompute。

    参数:
        doc: FreeCAD 文档对象
        base_name: 被剪切的基体对象名 (str)
        tool_defs: 工具定义列表，每项是 dict:
            {
                "type": "cylinder",      # cylinder / box / sphere / cone
                "name": "Tool_1",        # 唯一名称
                "radius": 6.0,
                "height": 20.0,
                "center": (x, y, z),     # 几何中心
                # box 用: "length", "width", "height", "center"
                # sphere 用: "radius", "center"
                # cone 用: "radius1", "radius2", "height", "center"
            }
        cut_name: 剪切结果名称

    返回:
        剪切结果对象
    """
    tools = []
    for td in tool_defs:
        t = _make_tool(doc, td)
        tools.append(t)

    if not tools:
        print("⚠️ 没有工具对象，返回基体")
        return doc.getObject(base_name)

    # 融合所有工具为一个
    fuse = doc.addObject("Part::MultiFuse", "Fuse_Tools")
    fuse.Shapes = tools

    # 一次性剪切
    cut = doc.addObject("Part::Cut", cut_name)
    cut.Base = doc.getObject(base_name)
    cut.Tool = fuse

    doc.recompute()
    print(f"✅ multi_cut: {len(tools)} 个工具 → {cut_name}")
    return cut


def _make_tool(doc, td):
    """根据定义创建工具体素"""
    t = td["type"]
    name = td["name"]
    center = td.get("center", (0, 0, 0))

    if t == "cylinder":
        obj = doc.addObject("Part::Cylinder", name)
        obj.Radius = td["radius"]
        obj.Height = td["height"]
        obj.Placement.Base = App.Vector(
            center[0], center[1], center[2] - td["height"] / 2
        )
    elif t == "box":
        obj = doc.addObject("Part::Box", name)
        obj.Length = td["length"]
        obj.Width = td["width"]
        obj.Height = td["height"]
        obj.Placement.Base = App.Vector(
            center[0] - td["length"] / 2,
            center[1] - td["width"] / 2,
            center[2] - td["height"] / 2,
        )
    elif t == "sphere":
        obj = doc.addObject("Part::Sphere", name)
        obj.Radius = td["radius"]
        obj.Placement.Base = App.Vector(*center)
    elif t == "cone":
        obj = doc.addObject("Part::Cone", name)
        obj.Radius1 = td["radius1"]
        obj.Radius2 = td.get("radius2", 0)
        obj.Height = td["height"]
        obj.Placement.Base = App.Vector(
            center[0], center[1], center[2] - td["height"] / 2
        )
    else:
        raise ValueError(f"不支持的工具类型: {t}")

    return obj


# ============ 使用示例 ============
if __name__ == "__main__":
    doc = App.ActiveDocument
    if doc is None:
        doc = App.newDocument("BooleanTest")

    # 清理
    for obj in doc.Objects:
        doc.removeObject(obj.Name)

    # 1. 创建基体（法兰主体）
    base = doc.addObject("Part::Cylinder", "Base")
    base.Radius = 60
    base.Height = 16
    base.Placement.Base = App.Vector(0, 0, -8)

    # 2. 定义所有要剪切掉的工具
    tools = [
        # 中心孔
        {"type": "cylinder", "name": "CenterHole", "radius": 20, "height": 20, "center": (0, 0, 0)},
        # 6 个螺栓孔
        *[
            {
                "type": "cylinder", "name": f"BoltHole_{i+1}",
                "radius": 6, "height": 20,
                "center": (
                    45 * __import__("math").cos(__import__("math").radians(30 + i * 60)),
                    45 * __import__("math").sin(__import__("math").radians(30 + i * 60)),
                    0,
                ),
            }
            for i in range(6)
        ],
    ]

    # 3. 一次性剪切
    result = multi_cut(doc, "Base", tools, "Flange_Result")
    doc.recompute()
    print("🎉 完成！")
