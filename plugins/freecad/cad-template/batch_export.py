# batch_export.py — 批量导出对象为 STEP / STL
# 用法: 在 FreeCAD Python 控制台中粘贴运行
#       修改 export_dir 和目标格式，运行 main()

import FreeCAD as App
import os
import glob


def export_step(doc, objects=None, out_dir=None, prefix=""):
    """
    批量导出 STEP。

    参数:
        doc: FreeCAD 文档对象
        objects: 要导出的对象列表（None = 全部可见对象）
        out_dir: 输出目录（None = 文档所在目录 / 桌面）
        prefix: 文件名前缀

    返回:
        导出的文件路径列表
    """
    return _export(doc, objects, out_dir, prefix, "step")


def export_stl(doc, objects=None, out_dir=None, prefix=""):
    """批量导出 STL。参数同上。"""
    return _export(doc, objects, out_dir, prefix, "stl")


def _export(doc, objects, out_dir, prefix, fmt):
    if objects is None:
        objects = [o for o in doc.Objects if hasattr(o, "Shape") and o.Shape]

    if out_dir is None:
        try:
            out_dir = os.path.dirname(doc.FileName)
        except:
            out_dir = os.path.expanduser("~/Desktop")
    os.makedirs(out_dir, exist_ok=True)

    exported = []
    for obj in objects:
        safe_name = "".join(c if c.isalnum() or c in "._-" else "_" for c in obj.Label)
        fname = f"{prefix}{safe_name}.{fmt}" if fmt == "step" else f"{prefix}{safe_name}.stl"
        fpath = os.path.join(out_dir, fname)

        if fmt == "step":
            # 两种方式尝试
            try:
                import ImportGui
                ImportGui.export([obj], fpath)
            except:
                Part.export([obj], fpath)
        else:
            Part.export([obj], fpath)

        exported.append(fpath)
        print(f"  📦 {fpath}")

    print(f"✅ 导出 {len(exported)} 个 .{fmt} 文件到: {out_dir}")
    return exported


def export_all(doc, out_dir=None):
    """
    同时导出 STEP 和 STL。

    返回:
        (step_files, stl_files)
    """
    steps = export_step(doc, out_dir=out_dir)
    stls = export_stl(doc, out_dir=out_dir)
    return steps, stls


# ============ 使用示例 ============
if __name__ == "__main__":
    doc = App.ActiveDocument
    if doc is None:
        print("❌ 请先打开文档")
    else:
        # 导出所有对象
        export_step(doc, out_dir=os.path.expanduser("~/Desktop/FreeCAD_Export"))
        # 或者导出特定对象:
        # obj = doc.getObject("FlangeBody")
        # export_step(doc, objects=[obj])
