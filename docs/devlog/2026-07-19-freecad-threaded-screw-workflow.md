# FreeCAD 螺纹孔与螺栓建模工作流总结

> 日期: 2026-07-19 | 工程: Seelex FreeCAD MCP Plugin

---

## 1. 概述

本次工作流在 FreeCAD 中完成了一个 **带 M12 螺纹孔的 50mm 正方体** 和一颗 **配套的 M12 六角头螺栓** 的完整建模。全部操作通过 Seelex FreeCAD MCP Plugin 的 Python 脚本远程执行。

在此之前，还完成了 **100x50x20 mm 长方体带 Ø30 贯穿孔** 的建模作为前置验证。

---

## 2. 核心工具调用流程

### 2.1 前置验证阶段 — 100x50x20 开孔长方体

在开始螺纹建模之前，先完成了 100x50x20mm 长方体带 Ø30 贯穿孔的建模，作为 FreeCAD MCP 的功能验证。

| 序号 | 操作 | 描述 |
|------|------|------|
| 1a | 创建文档 & Body | `App.newDocument` + `App.ActiveDocument.addObject("PartDesign::Body")` |
| 1b | 草图失败 | `addObject("Sketcher::SketchObject")` 因环境问题报 `AttributeError` |
| 1c | 改用 Part 直接建模 | `Part::Box` -> 100x50x20 @ 原点 |
| 1d | 创建圆柱 | `Part::Cylinder` -> 半径 15mm (Ø30), 高 30mm |
| 1e | 定位圆柱 | Placement.Base 对齐到顶面中心 (50, 25, 20) |
| 1f | 布尔减 | `Part::Cut` -> 获得带贯穿孔长方体 |
| 1g | 截图尝试 | 3 次均失败 (`AttributeError`, 缺 PySide2, `getImage` 属性缺失) |
| 1h | 第二次验证 | 使用插件成功创建并截图 (等轴测视图) |

**关键发现：** Part 工作流的 `Part::Cut` / `Part::Fuse` 等布尔操作比 PartDesign 更稳定可靠，适合远程脚本执行。

### 2.2 螺纹孔+螺栓建模阶段 (会话 dbf35a75)

| 序号 | 操作 | 工具/方法 | 描述 |
|------|------|-----------|------|
| 1 | 创建正方体 | `Part::Box` | 50x50x50 mm, 原点 (0,0,0) |
| 2 | 创建通孔圆柱 | `Part::Cylinder` | 半径 10.2 mm (M12 底孔), 高 60 mm, 中心 (25,25,-5) |
| 3 | 布尔求差 | `Part::Cut` | 从正方体减去圆柱，获得通孔 |
| 4 | 创建螺纹螺旋线 | `Part::Helix` | 螺距 1.75 mm, 高 50 mm, 半径 6 mm, 中心 (25,25,0) |
| 5 | 创建螺纹截面 | `Part::Feature` -> 构建 `Part::TopoShape` | 三角形截面 (0.5mmx0.5mmx0.5mm 等腰直角三角形) |
| 6 | 创建扫掠 | `Part::Sweep` | 以螺旋线为路径扫掠截面 |
| 7 | 布尔求差 (内螺纹) | `Part::Cut` | 从带孔正方体减去扫掠体，获得内螺纹 |
| 8 | 创建螺杆 | `Part::Cylinder` | 半径 5.5 mm (M12 螺纹小径), 从 (25,25,10) 到 (25,25,48.25) |
| 9 | 创建外螺纹螺旋线 | `Part::Helix` | 螺距 1.75 mm, 高 38.25 mm, 半径 5.5 mm |
| 10 | 创建外螺纹截面 | 同上方法 | 三角形截面 |
| 11 | 扫掠外螺纹 | `Part::Sweep` | 得外螺纹实体 |
| 12 | 布尔融合 (外螺纹+螺杆) | `Part::Fuse` | 合并螺纹和螺杆 |
| 13 | 创建六角头 | `Part::RegularPolygon` + 拉伸 | 六边形+拉伸至 8mm |
| 14 | 最终融合 | `Part::Fuse` | 六角头 + 螺纹螺杆 -> `CompleteScrew` |
| 15 | 隐藏中间件 | 设置 Visibility | 仅保留 `ThreadedHole` 和 `CompleteScrew` 可见 |

### 2.3 文档状态查询阶段 (会话 8e5570a1)

| 序号 | 操作 | 工具/方法 | 描述 |
|------|------|-----------|------|
| 16 | 列出所有对象 | `execute_python` + `App.ActiveDocument.Objects` | 遍历所有对象，打印类型、可见性、位置、包围盒 |
| 17 | 尝试调用 `get_document_state` | FreeCAD 工具 | 返回错误：工具不存在 |

### 2.4 螺纹偏移修复阶段 (同一会话)

**问题现象：**
- `ThreadSweep` (内螺纹扫掠体) 的 Z 范围偏移至 **-50.87 ~ 0.87**，未能对齐正方体 (Z: 0~50)
- `ScrewThreadSweep` (外螺纹扫掠体) 的 Z 范围偏移至 **-39.13 ~ 0.88**，未能对齐螺杆 (Z: 10~48.25)

**修复过程：**

| 序号 | 操作 | 描述 |
|------|------|------|
| 18 | 删除依赖对象 | 删除 `ThreadedHole`, `CompleteScrew`, `ScrewBody` 以解除依赖锁定 |
| 19 | 调整内螺纹 Placement | `ThreadSweep.Placement.Base.z = 50.875` (将 Z 提升约 50.875mm) |
| 20 | 调整外螺纹 Placement | `ScrewThreadSweep.Placement.Base.z = 49.125` (将 Z 提升约 49.125mm) |
| 21 | 重新组合对象 | 重建 `ThreadedHole` (Cut), `ScrewBody` (Fuse), `CompleteScrew` (Fuse) |
| 22 | 恢复可见性 | 仅保留最终对象可见 |

**修复原理：**
扫掠体的几何数据是在局部坐标系中定义的，其原点 (0,0,0) 对应螺旋线的起点。由于螺旋线的起点在 Z=0 而扫掠体的局部 BoundBox 起始于负 Z，需要通过 Placement.Base.z 将其整体抬升到正确位置。

---

## 3. 关键技术细节

### 3.1 螺纹参数 (M12 x 1.75)

| 参数 | 值 |
|------|-----|
| 公称直径 (Major Diameter) | 12 mm |
| 螺距 (Pitch) | 1.75 mm |
| 螺纹小径 (Minor Diameter) | ~ 10.1 mm |
| 底孔钻径 (Tap Drill) | 10.2 mm |
| 螺旋线半径 (内螺纹) | 6 mm (Major/2) |
| 螺旋线半径 (外螺纹) | 5.5 mm (~Minor/2) |

### 3.2 螺纹截面几何

使用等腰直角三角形作为螺纹牙型：
- 两条直角边 = **0.5 mm**
- 位于 Helix 起始点切线平面
- 通过 `Part.__sortEdges__()` + `Part.Wire` + `Part.Face` 构建

### 3.3 扫掠关键参数

```python
# 创建扫掠
sweep = doc.addObject("Part::Sweep", "ThreadSweep")
sweep.Spine = helix       # 螺旋线作为路径
sweep.Sections = [profile] # 截面 (必须是 Part::Feature 对象)
sweep.Solid = True
sweep.Frenet = True
```

**注意：** 截面必须通过 `Part::Feature` 封装 (不能直接用 `Part::TopoShape`)，否则会报 `TypeError: PyObject is not a section shape`。

### 3.4 Placement 偏移修复的坑

- `Shape.BoundBox` 返回的是 **全局坐标系** 下的包围盒 (已包含 Placement 变换)
- 修改 `Placement.Base` 会触发 FreeCAD 自动重新计算 `BoundBox`
- 修复时直接设置 `Placement.Base.z` 即可，不需要修改几何本身
- 需先删除依赖当前对象的上层对象，否则 FreeCAD 拒绝修改

---

## 4. 遇到的问题与解决方案

### 4.1 连接超时 (context deadline exceeded)

多次出现 MCP 连接超时，原因是 FreeCAD 在 `recompute()` 时占用大量 CPU 导致无法及时响应。

**解决方案：**
- 减少每次 `recompute()` 后的查询操作
- 一次调用完成尽可能多的操作
- 分批执行，减少连接次数

### 4.2 FreeCAD 断路器 (Circuit Breaker)

连续连接失败后，MCP breaker 进入 `degraded (backoff)` 状态，拒绝所有请求直到退避时间到期。

**现象：**
```
mcp: server "freecad" is currently unavailable
(mcp breaker: server "freecad" is degraded (backoff until 01:27:27))
```

**处理：**
- 使用 `get_time()` 确认当前时间是否已过退避截止时间
- 到期后自动恢复，但可能立即再次超时

### 4.3 FreeCADCmd 进程崩溃

在早期会话中，设置源对象 `Visibility = False` 后 FreeCAD 进程崩溃：
```
系统找不到 FreeCADCmd
```

导致后续所有操作中断，需要手动重启 FreeCAD 和 MCP 服务。

### 4.4 Quantity 单位运算错误

```
TypeError: unsupported operand type(s) for +: 'Quantity' and 'float'
```

FreeCAD 的 `Placement.Base.z` 返回的是 `Quantity` 类型 (带 mm 单位)，不能直接与 float 相加。

**解决方案：**
```python
z = obj.Placement.Base.z  # Quantity('50.875 mm')
z_mm = z.getValueAs("mm").Value  # 或 z.Value -> float
# 或者直接用属性赋值，FreeCAD 会自动转换
obj.Placement.Base.z = 50.875  # 可行，FreeCAD 接受 float
```

### 4.5 TypeError: PyObject is not a section shape

直接传递 `Part.TopoShape` 作为扫掠截面时报错。

**解决方案：**
使用 `Part::Feature` 封装截面：
```python
profile_obj = doc.addObject("Part::Feature", "ThreadProfile")
profile_obj.Shape = face  # TopoShape
sweep.Sections = [profile_obj]  # OK
```

---

## 5. 最终文档状态

### 可见对象 (最终)

| 对象名 | 类型 | 描述 |
|--------|------|------|
| `ThreadedHole` | `Part::Cut` | 带完美内螺纹的 50mm 正方体 |
| `CompleteScrew` | `Part::Fuse` | M12 六角头螺栓 (螺纹段+螺杆+六角头) |

### 隐藏的中间对象

```
Part__Box        - 原始正方体
TapHole          - 通孔圆柱
HoleCut          - 布尔减结果
ThreadHelix      - 内螺纹螺旋线
ThreadProfile    - 内螺纹截面
ThreadSweep      - 内螺纹扫掠体
ScrewHelix       - 外螺纹螺旋线
ScrewThreadProfile - 外螺纹截面
ScrewThreadSweep - 外螺纹扫掠体
ScrewShaft       - 螺杆
ScrewBody        - 螺杆+外螺纹融合
HexHead          - 六边形草图
HexHeadExtrude   - 六角头拉伸体
```

---

## 6. 经验教训

1. **先查后改**：修改对象前先打印 Placement 和 BoundBox 确认当前状态
2. **安全取值**：处理 FreeCAD 属性时始终考虑 Quantity 与 float 的兼容性
3. **依赖链管理**：删除上层对象前需先解除依赖 (Visibility 不影响依赖关系)
4. **分批提交**：FreeCAD recompute 开销大，应合并操作减少 recompute 次数
5. **BoundBox 陷阱**：Shape.BoundBox 返回全局坐标，修改 Placement 后自动重算
6. **Part vs PartDesign**：Part 工作流 (Part::Box/Cut/Fuse) 比 PartDesign (Body/Sketch) 更稳定，适合远程脚本执行
7. **断路器机制**：MCP breaker 有退避机制，连接失败后需等待退避时间到期
8. **进程稳定性**：避免在 FreeCAD 繁忙时执行 Visibility 等操作，可能触发进程崩溃
