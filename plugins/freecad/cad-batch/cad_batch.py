# cad_batch.py — 批量 CAD 执行器
# 读取 JSON 操作序列，一次性在 FreeCAD 中执行全部操作。
# 用法: FreeCADCmd cad_batch.py --params params.json --output result.step
#
# 核心目的：省 token（JSON 比 Python 紧凑）+ 降 MCP 故障率（一次 recompute）

import sys, os, json, argparse

# 定位 freecad_core.py
SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
CORE_PATH = os.path.join(SCRIPT_DIR, '..', 'cad-core', 'freecad_core.py')
exec(open(CORE_PATH).read())


def execute_operations(doc, operations):
    """按序执行操作列表，返回最后创建的对象名。"""
    last_name = None
    for i, op in enumerate(operations):
        t = op.get("type", "")
        name = op.get("name", f"Op_{i}")
        try:
            if t == "box":
                make_box(doc, name, op["length"], op["width"], op["height"], op.get("center"))
            elif t == "cylinder":
                make_cylinder(doc, name, op["radius"], op["height"], op.get("center"))
            elif t == "sphere":
                make_sphere(doc, name, op["radius"], op.get("center"))
            elif t == "cone":
                make_cone(doc, name, op["radius1"], op["radius2"], op["height"], op.get("center"))
            elif t == "torus":
                make_torus(doc, name, op["radius1"], op["radius2"], op.get("center"))
            elif t == "helix":
                make_helix(doc, name, op["pitch"], op["height"], op["radius"], op.get("center"))
            elif t == "cut":
                bool_cut(doc, name, op["base"], op["tool"])
            elif t == "fuse":
                bool_fuse(doc, name, op["base"], op["tool"])
            elif t == "common":
                bool_common(doc, name, op["base"], op["tool"])
            elif t == "multi_cut":
                multi_cut(doc, op["base"], op["tools"], name)
            elif t == "circular_array":
                # 需要 template 对象已存在
                tmpl = doc.getObject(op["template"])
                circular_array(doc, tmpl, op["count"], op["radius"],
                               op.get("start_angle", 0), op.get("name_prefix", "Copy"))
            elif t == "linear_array":
                tmpl = doc.getObject(op["template"])
                linear_array(doc, tmpl, op["count"], op["step"],
                             op.get("direction", (1, 0, 0)), op.get("name_prefix", "Copy"))
            elif t == "mirror":
                mirror_obj(doc, op["source"], op.get("plane", "XY"), op.get("offset", 0))
            elif t == "move":
                pos = op.get("position", op)  # 兼容 {x,y,z} 直接写在 op 里
                move_to(doc, name, pos.get("x", 0), pos.get("y", 0), pos.get("z", 0))
            elif t == "translate":
                translate(doc, name, op.get("dx", 0), op.get("dy", 0), op.get("dz", 0))
            elif t == "rotate":
                rotate_obj(doc, name, op["axis"], op["angle"], op.get("center"))
            elif t == "fillet":
                _apply_fillet(doc, op)
            elif t == "delete":
                delete_obj(doc, name)
            elif t == "hide":
                hide_obj(doc, name)
            elif t == "show":
                show_obj(doc, name)
            elif t == "save":
                save_doc(doc, op.get("path"))
            elif t == "export_step":
                export_step(doc, op.get("objects"), op.get("out_path"))
            elif t == "export_stl":
                export_stl(doc, op.get("objects"), op.get("out_path"))
            elif t == "inspect":
                if name and doc.getObject(name):
                    inspect_obj(doc, name)
                else:
                    list_all(doc)
            elif t == "recompute":
                recompute(doc)
            else:
                print(f"⚠️  未知操作类型: {t}，跳过")
            last_name = name
        except Exception as e:
            print(f"❌ 操作 [{i}] {t}/{name} 失败: {e}")
            # 继续执行后续操作（不中断）
    return last_name


def _apply_fillet(doc, op):
    """对 base 对象按 edge_filter 筛选边并倒圆角。"""
    base_name = op["base"]
    radius = op.get("radius", 2.0)
    edge_filter = op.get("edge_filter", {})
    obj = doc.getObject(base_name)
    edges = obj.Shape.Edges
    sel = []
    import math
    min_len = edge_filter.get("min_length", 0)
    max_len = edge_filter.get("max_length", float("inf"))
    min_r = edge_filter.get("min_r", 0)
    max_r = edge_filter.get("max_r", float("inf"))
    for i, e in enumerate(edges):
        try:
            if e.Length < min_len or e.Length > max_len:
                continue
            cog = e.CenterOfGravity
            r = math.sqrt(cog.x**2 + cog.y**2)
            if r < min_r or r > max_r:
                continue
            sel.append((i + 1, radius, radius))
        except:
            pass
    if not sel:
        print(f"⚠️  fillet: 没有匹配的边")
        return
    fillet = doc.addObject("Part::Fillet", op.get("name", f"Fillet_{base_name}"))
    fillet.Base = obj
    fillet.Edges = sel
    doc.recompute()
    print(f"🔧 {fillet.Name}: fillet {len(sel)} edges R{radius}")


def main():
    parser = argparse.ArgumentParser(description="批量 CAD 执行器")
    parser.add_argument("--params", "-p", required=True, help="JSON 操作参数文件路径")
    parser.add_argument("--output", "-o", help="导出路径（STEP/STL，根据扩展名自动判断）")
    parser.add_argument("--doc", help="已有 FreeCAD 文档路径（不指定则新建）")
    args = parser.parse_args()

    # 读取参数
    with open(args.params, "r", encoding="utf-8") as f:
        params = json.load(f)
    operations = params.get("operations", [])

    # 打开或创建文档
    if args.doc and os.path.exists(args.doc):
        import FreeCAD
        doc = FreeCAD.open(args.doc)
        print(f"📂 加载文档: {args.doc}")
    else:
        doc_name = params.get("doc_name", "BatchCAD")
        doc = new_doc(doc_name)

    # 执行全部操作（先创建，不 recompute）
    print(f"🔧 执行 {len(operations)} 个操作...")
    last_name = execute_operations(doc, operations)

    # 最终输出
    if args.output:
        ext = os.path.splitext(args.output)[1].lower()
        obj_names = params.get("export_objects")
        out_dir = os.path.dirname(args.output) or "."
        if ext == ".step":
            export_step(doc, obj_names, out_dir)
        elif ext == ".stl":
            export_stl(doc, obj_names, out_dir)
        else:
            save_doc(doc, args.output)

    # 报告
    if last_name and doc.getObject(last_name):
        inspect_obj(doc, last_name)

    print("✅ 批量执行完成")


if __name__ == "__main__":
    main()
