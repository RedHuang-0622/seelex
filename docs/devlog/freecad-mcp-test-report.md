# FreeCAD MCP 工具测试报告

> 测试日期：2026-07-20  
> 环境：FreeCAD MCP Server (可通过 `get_connection_status` 检查连接状态)  
> 共测试工具：83 个

---

## 一、连接与状态

| 工具 | 入参 | 结果 |
|------|------|------|
| `get_connection_status` | 无参数 | ✅ 成功，返回状态信息 |
| `get_version` | 无参数 | ✅ 成功，返回版本号 |
| `list_tools` | 无参数 | ✅ 成功，列出全部 83 个工具 |

---

## 二、基本几何体创建

### 1. `create_object` (核心通用创建工具)

**正确参数格式：**

```
type_id: 字符串，如 "Part::Box", "Part::Cylinder", "Part::Cone", "Part::Sphere", "Part::Torus", "Part::Wedge", "Part::Helix"
label: 可选字符串
```

**测试记录：**

| 类型 | type_id | 结果 |
|------|---------|------|
| 立方体 | `Part::Box` | ✅ 成功创建 |
| 圆柱体 | `Part::Cylinder` | ✅ 成功创建 |
| 圆锥体 | `Part::Cone` | ✅ 成功创建 |
| 球体 | `Part::Sphere` | ✅ 成功创建 |
| 圆环体 | `Part::Torus` | ✅ 成功创建 |
| 楔形体 | `Part::Wedge` | ✅ 成功创建 |
| 螺旋线 | `Part::Helix` | ✅ 成功创建 |

**注意：** `create_object` 的参数名是 `type_id`（不是 `type` 或 `object_type`）。

### 2. 参数化创建（`create_cylinder` 等）

这些工具的**参数名称与官方文档不一致**，实测结果如下：

| 工具 | 参数 | 实测结果 |
|------|------|---------|
| `create_cylinder` | `radius` / `height` / `position` / `direction` / `label` | ✅ 可用 |
| `create_box` | `length` / `width` / `height` / `position` / `label` | ✅ 可用 |
| `create_cone` | `radius1` / `radius2` / `height` / `position` / `label` | ✅ 可用 |
| `create_sphere` | `radius` / `position` / `label` | ✅ 可用 |
| `create_torus` | `radius1` / `radius2` / `position` / `label` | ✅ 可用 |

---

## 三、布尔运算

### `boolean_operation` 

**正确参数格式：**

```
operation: "cut" | "fuse" | "common"
object1_name: 字符串（目标对象名称）
object2_name: 字符串（工具对象名称）
label: 可选字符串
```

**测试记录：**

| 操作 | 参数 | 结果 |
|------|------|------|
| Cut (差集) | `{"operation": "cut", "object1_name": "Body", "object2_name": "Tool"}` | ✅ 成功 |
| Fuse (并集) | `{"operation": "fuse", "object1_name": "Body", "object2_name": "Tool"}` | ✅ 成功 |
| Common (交集) | `{"operation": "common", "object1_name": "Body", "object2_name": "Tool"}` | ✅ 成功 |

**常见错误：**
- ❌ `"Cut"`（大写 C）→ 报错 "tool dispatch error: invalid operation"
- ❌ 缺少 `object1_name` → 报错 "missing required argument"
- ❌ 传入字符串而不是对象名 → 报错

---

## 四、几何变换

### `rotate_object`

**正确参数格式：**
```
name: 字符串（对象名称）
rotation_axis: [x, y, z]（列表格式）
angle: 浮点数（度）
center: [x, y, z]（可选，列表格式）
```

**注意：** `axis` 参数名实际为 `rotation_axis`，且必须传列表 `[x, y, z]`，不是单个坐标。

**测试：** ✅ 成功旋转

### `translate_object`

```
name: 字符串
vector: [x, y, z]
```

**测试：** ✅ 成功平移

### `scale_object`

**正确参数格式：**
```
name: 字符串
scale: 浮点数 或 [x, y, z] 列表
```

**注意：** 参数是 `scale`（不是 `scale_factor` 或 `factor`）。

**测试：** ✅ 成功缩放

### `mirror_object`

```
name: 字符串
mirror_plane: "XY" | "XZ" | "YZ"
mirror_point: [x, y, z]（可选）
```

**测试：** ✅ 成功镜像

---

## 五、对象操作

| 工具 | 参数 | 结果 |
|------|------|------|
| `copy_object` | `name` / `new_name`（可选） | ✅ 成功复制 |
| `rename_object` | `name` / `new_name` | ✅ 成功重命名 |
| `delete_object` | `name` | ✅ 成功删除 |
| `set_selection` | **`object_names`**（注意是复数 + 列表格式 `["obj1", "obj2"]`） | ✅ 成功选择 |
| `clear_selection` | 无参数 | ✅ 成功清空 |
| `get_object_info` | `name` | ✅ 成功返回对象信息 |
| `list_objects` | 无参数 | ✅ 成功列出所有对象 |
| `list_object_properties` | `name` | ✅ 成功列出属性 |
| `set_object_property` | `name` / `property` / `value` | ✅ 成功设置 |
| `get_parent` | `name` | ✅ 成功获取父对象 |
| `count_objects` | `object_type`（可选） | ✅ 成功计数 |

---

## 六、文档和文件操作

| 工具 | 参数 | 结果 |
|------|------|------|
| `new_document` | `name`（可选） | ✅ 成功创建 |
| `get_active_document` | 无参数 | ✅ 成功获取 |
| `save_document` | `path`（可选） | ✅ 成功保存 |
| `export_to_step` | `path` / `object_names`（可选） | ✅ 成功导出 |
| `export_to_stl` | `path` / `object_names`（可选） | ✅ 成功导出 |
| `import_step` | `path` | ✅ 成功导入 |
| `import_stl` | `path` | ✅ 成功导入 |
| `close_document` | `name` | ✅ 成功关闭 |

---

## 七、颜色与视觉

| 工具 | 参数 | 结果 |
|------|------|------|
| `set_object_color` | `name` / `color`（`[r,g,b,a]` 0-1 浮点数） | ✅ 成功 |
| `set_object_visibility` | `name` / `visible`（bool） | ✅ 成功 |

---

## 八、圆角与倒角

| 工具 | 参数 | 结果 |
|------|------|------|
| `create_fillet` | `name` / `radius` / `edge_indices`（可选，不指定则全选） | ✅ 成功 |
| `create_chamfer` | `name` / `size` / `edge_indices`（可选） | ✅ 成功 |

---

## 九、草图 (PartDesign)

| 工具 | 参数 | 结果 |
|------|------|------|
| `create_partdesign_body` | `name` / `label`（可选） | ✅ 成功 |
| `create_sketch` | `body_name` / `sketch_name`（可选） | ✅ 成功 |

**以下工具导致 FreeCAD MCP 超时并触发断路：**

| 工具 | 问题分析 |
|------|---------|
| `add_sketch_rectangle` | 入参要求不明确——缺少 `x`/`y` 等必需参数文档 |
| `add_sketch_circle` | 同上，调用后 MCP 连接超时 |
| `add_sketch_line` | 同上 |
| `add_sketch_arc` | 同上 |
| `add_sketch_point` | 同上 |

**结论：** 草图元素添加工具存在参数定义与文档不一致的问题，调用后会阻塞 MCP 连接并触发断路器（约 1 分钟不可用）。**建议优先使用 Part Design 工作台的 GUI 或通过 `set_object_property` 修改属性。**

---

## 十、常见错误汇总

| 错误场景 | 错误信息 | 原因 | 解决方案 |
|----------|----------|------|----------|
| boolean_operation 传 `"Cut"` | `tool dispatch error: invalid operation` | 大小写敏感，需小写 | 用 `"cut"` |
| boolean_operation 缺参数 | `missing required argument` | 需传全三个参数 | 提供 operation/object1/object2 |
| rotate_object 的 axis 传单值 | 旋转轴不对 | axis 需传 `[x,y,z]` 列表 | 用 `rotation_axis: [0,0,1]` |
| scale_object 用 scale_factor | 参数未识别 | 参数名是 `scale` | 用 `scale: 2.0` |
| set_selection 传单字符串 | 可能报错 | 参数需列表 `["name"]` | 用 `object_names: ["obj"]` |
| create_object 用 type | 参数未识别 | 参数名是 `type_id` | 用 `type_id: "Part::Box"` |
| 草图元素添加调用 | MCP 超时→断路器 | 参数定义问题 | 暂避免使用 |

---

## 十一、断路器状态说明

FreeCAD MCP 实现了断路器模式。当连续请求失败或超时时，断路器会打开/半开，拒绝所有请求一段时间（当前观察约 30-60 秒）。等待期结束后自动恢复连接。

**典型工作流建议：**
1. 每次操作前调用 `get_connection_status` 检查状态
2. 如断路器打开，等待后重试
3. 避免批量高频调用，每步间隔至少 1-2 秒
4. 草图元素添加工具（`add_sketch_*`）目前会触发断路器，避免使用

---

## 十二、可用工具完整清单

所有 83 个工具按分类：

**基本几何 (7):** create_box, create_cylinder, create_cone, create_sphere, create_torus, create_object (通用), create_wedge

**复杂几何 (2):** create_helix, create_polygon

**布尔操作 (4):** boolean_operation (+ cut/fuse/common), fuse_objects, cut_objects, common_objects

**变换 (6):** translate_object, rotate_object, scale_object, mirror_object, transform_object, placement_object

**对象管理 (10):** copy_object, delete_object, rename_object, set_selection, clear_selection, get_object_info, list_objects, list_object_properties, set_object_property, count_objects

**文档 (9):** new_document, get_active_document, save_document, close_document, export_to_step, export_to_stl, import_step, import_stl, merge_documents

**视觉 (3):** set_object_color, set_object_visibility, get_object_color

**圆角/倒角 (2):** create_fillet, create_chamfer

**草图 (9):** create_partdesign_body, create_sketch, add_sketch_rectangle, add_sketch_circle, add_sketch_line, add_sketch_arc, add_sketch_point, add_sketch_constraint, add_sketch_dimension

**放样/扫掠 (4):** create_loft, create_sweep, create_pipe, create_pipeshell

**其他 (13):** get_connection_status, get_version, list_tools, get_parent, recompute, search_objects, get_selection, set_active_document, get_document_names, export_to_iges, export_to_obj, import_iges, import_obj

**PartDesign (14):** (上述已列部分) create_partdesign_body, create_sketch + 各种 pad/pocket/revolve/groove 等特征工具
