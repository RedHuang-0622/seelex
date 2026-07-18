# FreeCAD 执行底座 — freecad 包 & Python MCP Server 详细设计

> **⚠️ 本文已过时**
>
> `freecad/server/` Python MCP Server 已删除，FreeCAD 改用**现有开源 MCP Server**（配置于 `mcp_servers`，与 WebSearch 同一生态位）。`freecad/` 包已降级为纯参数验证层（`PreValidate/PostValidate`），不做 MCP 通信。
> 见 [`../arch/design-decisions-mcp-storage.md`](../arch/design-decisions-mcp-storage.md) 第 3 节。

> 版本: v1.0  
> 创建日期: 2025-07-18  
> 状态: ❌ 已废弃（`server/` 已删除，freecad 降级为验证层）  
> 关联文档: `cad-architecture-overview.md`, `cad-command-stack.md`, `cad-mcp-bridge.md`

---

## 目录

1. [设计目标](#1-设计目标)
2. [整体架构](#2-整体架构)
3. [Go 端：freecad 包](#3-go-端freecad-包)
4. [Python 端：MCP Server](#4-python-端mcp-server)
5. [工具契约](#5-工具契约)
6. [FreeCAD 文档生命周期](#6-freecad-文档生命周期)
7. [容错与恢复](#7-容错与恢复)
8. [环境要求](#8-环境要求)
9. [完整文件骨架](#9-完整文件骨架)

---

## 1. 设计目标

- **headless 运行**：FreeCAD 在无 GUI 模式下作为服务运行
- **工具即操作**：每个 CAD 原子操作包装为 MCP 工具（草图、拉伸、开孔等）
- **幂等恢复**：支持从命令栈重放，每种操作可重复执行
- **错误可读**：FreeCAD 错误信息被 MCP Server 捕获并格式化为结构化的 LLM 友好文本
- **可扩展**：新 CAD 操作只需在 Server 端添加 Python 函数 + 注册工具

### 非目标

- 不实现 FreeCAD 内部 Python API 的深度封装——直接调用 FreeCAD Python
- 不处理实时渲染——FreeCAD headless 无渲染，导出 STL/STEP 即可
- 不管理多个并发设计——每个 MCP Server 实例对应一个设计

---

## 2. 整体架构

```
Seelex (Go)
    │
    │  MCP (stdio / JSON-RPC)
    │
    ▼
┌─────────────────────────────────────────────────────────────────┐
│              freecad_mcp_server.py (Python)                     │
│                                                                 │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │               MCP 协议层                                 │    │
│  │  JSON-RPC 解析 │ 请求路由 │ 响应序列化 │ 错误处理        │    │
│  └──────────────────────────┬──────────────────────────────┘    │
│                             │                                    │
│  ┌──────────────────────────▼──────────────────────────────┐    │
│  │               CAD 操作层                                 │    │
│  │  sketch_rectangle()  │  pad_extrude()  │  pocket()       │    │
│  │  fillet()  │  chamfer()  │  export_stl()  │  save()      │    │
│  └──────────────────────────┬──────────────────────────────┘    │
│                             │                                    │
│  ┌──────────────────────────▼──────────────────────────────┐    │
│  │               FreeCAD Python API                        │    │
│  │  App │  Part │  PartDesign │  Sketcher │  Mesh          │    │
│  └─────────────────────────────────────────────────────────┘    │
│                                                                 │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │               FreeCAD (headless)                        │    │
│  │  Document │  Body │  Feature │  SketchObject           │    │
│  └─────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────┘
```

---

## 3. Go 端：freecad 包

Go 端的 `freecad/` 包 **不包含任何 FreeCAD 调用逻辑**，职责是：

1. 定义 CAD 操作的类型、参数 Schema（JSON Schema 格式）
2. 提供参数验证函数
3. 管理操作类型常量（与 `commandstack.Op*` 共用同一组常量）

### 3.1 操作 Schema 定义

```go
package freecad

import "encoding/json"

// ── 操作参数 Schema ──────────────────────────────────────────

// SketchRectangleParams 矩形草图参数
type SketchRectangleParams struct {
    Plane  string  `json:"plane"`             // "XY" | "XZ" | "YZ" 或自定义平面名
    Length float64 `json:"length"`            // 长度 (mm)
    Width  float64 `json:"width"`             // 宽度 (mm)
    X      float64 `json:"x,omitempty"`       // 中心 X (默认 0)
    Y      float64 `json:"y,omitempty"`       // 中心 Y (默认 0)
}

// PadExtrudeParams 拉伸凸台参数
type PadExtrudeParams struct {
    SketchSeq int     `json:"sketch_seq"`            // 引用草图 seq
    Height    float64 `json:"height"`                // 拉伸高度 (mm)
    Direction string  `json:"direction,omitempty"`   // "Z" | "-Z" (默认 "Z")
    Reversed  bool    `json:"reversed,omitempty"`    // 反向
    TaperAngle float64 `json:"taper_angle,omitempty"` // 拔模角度
}

// PocketCircularParams 圆形开孔参数
type PocketCircularParams struct {
    SketchSeq int     `json:"sketch_seq"`            // 引用包含圆的草图 seq
    Depth     float64 `json:"depth"`                 // 开孔深度 (mm)
    Through   bool    `json:"through,omitempty"`     // 穿透所有
    Reversed  bool    `json:"reversed,omitempty"`    // 反向
}

// FilletParams 倒圆角参数
type FilletParams struct {
    EdgeIDs  []int   `json:"edge_ids"`            // 边 ID 列表
    Radius   float64 `json:"radius"`              // 圆角半径 (mm)
}

// ExportSTLParams 导出 STL 参数
type ExportSTLParams struct {
    FilePath string `json:"file_path"`            // 输出路径
    BodyName string `json:"body_name,omitempty"`  // 体对象名 (默认整个文档)
}

// ── Schema 注册表 ────────────────────────────────────────────

// OpSchema 描述一个操作的参数 Schema
type OpSchema struct {
    Op          string            `json:"op"`           // 操作名
    Description string            `json:"description"`  // 描述
    ParamSchema *json.RawMessage  `json:"param_schema"` // JSON Schema
}

// AllSchemas 所有支持的 CAD 操作 Schema
var AllSchemas = []OpSchema{
    {
        Op:          "sketch_rectangle",
        Description: "在指定平面上创建矩形草图",
        ParamSchema: mustSchema(SketchRectangleParams{}),
    },
    {
        Op:          "pad_extrude",
        Description: "将草图拉伸为实体",
        ParamSchema: mustSchema(PadExtrudeParams{}),
    },
    // ... 更多操作
}

func mustSchema(v interface{}) *json.RawMessage {
    data, err := json.Marshal(v)
    if err != nil {
        panic(err)
    }
    raw := json.RawMessage(data)
    return &raw
}
```

### 3.2 参数验证

```go
package freecad

import (
    "encoding/json"
    "fmt"
)

// Validate 验证操作参数。
// op 是操作名，params 是 JSON 参数。
// 返回解析后的参数对象和错误。
func Validate(op string, params json.RawMessage) (interface{}, error) {
    switch op {
    case OpSketchRectangle:
        var p SketchRectangleParams
        if err := json.Unmarshal(params, &p); err != nil {
            return nil, fmt.Errorf("sketch_rectangle: %w", err)
        }
        if p.Length <= 0 {
            return nil, fmt.Errorf("sketch_rectangle: length must be positive, got %f", p.Length)
        }
        if p.Width <= 0 {
            return nil, fmt.Errorf("sketch_rectangle: width must be positive, got %f", p.Width)
        }
        return &p, nil

    case OpPadExtrude:
        var p PadExtrudeParams
        if err := json.Unmarshal(params, &p); err != nil {
            return nil, fmt.Errorf("pad_extrude: %w", err)
        }
        if p.SketchSeq <= 0 {
            return nil, fmt.Errorf("pad_extrude: sketch_seq must be positive")
        }
        if p.Height <= 0 {
            return nil, fmt.Errorf("pad_extrude: height must be positive")
        }
        return &p, nil

    // ... 更多验证

    default:
        return nil, fmt.Errorf("unknown operation: %s", op)
    }
}
```

### 3.3 操作类型常量（与 commandstack 共享）

```go
package freecad

// 操作类型常量 —— 与 commandstack.Op* 保持一致
// 实际引入 commandstack 包即可
const (
    OpSketchRectangle  = "sketch_rectangle"
    OpSketchCircle     = "sketch_circle"
    OpPadExtrude       = "pad_extrude"
    OpPocketCircular   = "pocket_circular"
    OpFillet           = "fillet"
    OpChamfer          = "chamfer"
    OpExportSTL        = "export_stl"
    OpExportSTEP       = "export_step"
    OpSaveFCStd        = "save_fcstd"
)
```

### 3.4 Go 端的目录结构

```
freecad/
├── freecad.go        # 包声明、常量
├── ops.go            # 操作参数类型定义（SketchRectangleParams 等）
├── ops_test.go       # 参数类型 JSON 序列化测试
├── schema.go         # AllSchemas 注册表
├── validate.go       # Validate 函数
└── validate_test.go  # 参数验证测试（边界条件）
```

---

## 4. Python 端：MCP Server

Python 端的 MCP Server 是实际执行 FreeCAD 操作的进程。

### 4.1 代码结构

```
freecad/server/
├── server.py            # MCP Server 主入口（JSON-RPC 循环）
├── mcp_protocol.py      # MCP 协议层（JSON-RPC 解析/序列化）
├── operations/
│   ├── __init__.py
│   ├── sketches.py      # 草图操作（矩形、圆、线段）
│   ├── features.py      # 特征操作（拉伸、开孔、旋转）
│   ├── modifiers.py     # 修饰操作（倒角、圆角、镜像）
│   └── io.py            # 导入导出（STL、STEP、FCStd）
├── freecad_manager.py   # FreeCAD 文档/状态管理
├── utils.py             # 工具函数
├── requirements.txt     # Python 依赖
└── test/
    ├── test_sketches.py
    ├── test_features.py
    └── test_server.py
```

### 4.2 MCP Server 主循环

```python
# server.py — MCP Server 主入口

import sys
import json
import traceback
from freecad_manager import FreeCADManager

class MCPServer:
    """MCP 协议服务器，通过 stdio 与 Go 客户端通信。"""

    def __init__(self):
        self.fc = FreeCADManager()
        self.handlers = {}
        self._register_handlers()

    def _register_handlers(self):
        """注册所有 CAD 操作处理器。"""
        from operations.sketches import handle_sketch_rectangle, handle_sketch_circle
        from operations.features import handle_pad_extrude, handle_pocket_circular
        from operations.modifiers import handle_fillet, handle_chamfer
        from operations.io import handle_export_stl, handle_export_step, handle_save_fcstd

        self.handlers = {
            "sketch_rectangle": handle_sketch_rectangle,
            "sketch_circle":    handle_sketch_circle,
            "pad_extrude":      handle_pad_extrude,
            "pocket_circular":  handle_pocket_circular,
            "fillet":           handle_fillet,
            "chamfer":          handle_chamfer,
            "export_stl":       handle_export_stl,
            "export_step":      handle_export_step,
            "save_fcstd":       handle_save_fcstd,
        }

    def handle_request(self, request: dict) -> dict:
        """处理单个 JSON-RPC 请求。"""
        req_id = request.get("id")
        method = request.get("method", "")
        params = request.get("params", {})

        try:
            if method == "initialize":
                return self._handle_initialize(req_id, params)
            elif method == "tools/list":
                return self._handle_tools_list(req_id)
            elif method == "tools/call":
                return self._handle_tools_call(req_id, params)
            elif method == "shutdown":
                return self._handle_shutdown(req_id)
            elif method == "notifications/initialized":
                return None  # 无响应
            else:
                return self._error(req_id, -32601, f"Method not found: {method}")
        except Exception as e:
            return self._error(req_id, -32603, f"Internal error: {e}\n{traceback.format_exc()}")

    def _handle_tools_list(self, req_id):
        """返回支持的工具列表。"""
        tools = []
        for name, handler in self.handlers.items():
            tools.append({
                "name": name,
                "description": handler.__doc__ or "",
                "inputSchema": handler.schema if hasattr(handler, "schema") else {"type": "object"},
            })
        return self._result(req_id, {"tools": tools})

    def _handle_tools_call(self, req_id, params):
        """调用指定工具。"""
        name = params.get("name", "")
        arguments = params.get("arguments", {})

        if name not in self.handlers:
            return self._error(req_id, -32601, f"Tool not found: {name}")

        handler = self.handlers[name]
        result = handler(self.fc, arguments)
        return self._result(req_id, {"content": [{"type": "text", "text": json.dumps(result)}]})

    def run(self):
        """主循环：从 stdin 读取 JSON-RPC 请求并响应。"""
        self.fc.initialize()
        for line in sys.stdin:
            line = line.strip()
            if not line:
                continue
            try:
                request = json.loads(line)
                response = self.handle_request(request)
                if response is not None:
                    sys.stdout.write(json.dumps(response) + "\n")
                    sys.stdout.flush()
            except json.JSONDecodeError:
                err = self._error(None, -32700, "Parse error")
                sys.stdout.write(json.dumps(err) + "\n")
                sys.stdout.flush()


if __name__ == "__main__":
    server = MCPServer()
    server.run()
```

### 4.3 FreeCAD 管理器

```python
# freecad_manager.py — FreeCAD 文档生命周期管理

import FreeCAD
import Part
import PartDesign
import Sketcher

class FreeCADManager:
    """管理一个 headless FreeCAD 文档实例。"""

    def __init__(self):
        self.doc = None
        self.body = None
        self.feature_count = 0

    def initialize(self):
        """初始化 FreeCAD 文档。"""
        FreeCAD.newDocument("SeelexDesign")
        self.doc = FreeCAD.ActiveDocument
        self.body = self.doc.addObject("PartDesign::Body", "Body")
        self.feature_count = 0

    def new_sketch(self, plane: str) -> Sketcher.SketchObject:
        """在指定平面上创建新草图。"""
        sketch = self.doc.addObject("Sketcher::SketchObject", f"Sketch_{self.feature_count}")
        if plane == "XY":
            sketch.Support = (self.doc.XY_Plane, [""])
        elif plane == "XZ":
            sketch.Support = (self.doc.XZ_Plane, [""])
        elif plane == "YZ":
            sketch.Support = (self.doc.YZ_Plane, [""])
        else:
            raise ValueError(f"Unsupported plane: {plane}")
        self.body.addObject(sketch)
        self.feature_count += 1
        return sketch

    def get_sketch_by_seq(self, seq: int):
        """按 seq 序号查找草图对象。"""
        name = f"Sketch_{seq}"
        return self.doc.getObject(name)

    def recompute(self):
        """重新计算文档。"""
        self.doc.recompute()
```

### 4.4 操作处理器示例

```python
# operations/sketches.py — 草图操作

def handle_sketch_rectangle(fc, params):
    """
    创建矩形草图。

    Parameters:
        plane (str): 平面 "XY" | "XZ" | "YZ"
        length (float): 长度 (mm)
        width (float): 宽度 (mm)
        x (float, optional): 中心 X，默认 0
        y (float, optional): 中心 Y，默认 0

    Returns:
        dict: { "sketch_id": int, "status": "ok" }
    """
    sketch = fc.new_sketch(params["plane"])

    length = float(params["length"])
    width = float(params["width"])
    x = float(params.get("x", 0))
    y = float(params.get("y", 0))

    # 矩形四个角
    p1 = (x - length/2, y - width/2)
    p2 = (x + length/2, y - width/2)
    p3 = (x + length/2, y + width/2)
    p4 = (x - length/2, y + width/2)

    # 创建四条线段
    lines = []
    for start, end in [(p1, p2), (p2, p3), (p3, p4), (p4, p1)]:
        line = sketch.addGeometry(
            Part.LineSegment(
                FreeCAD.Vector(start[0], start[1], 0),
                FreeCAD.Vector(end[0], end[1], 0)
            ),
            False
        )
        lines.append(line)

    fc.recompute()
    return {"sketch_id": fc.feature_count - 1, "status": "ok"}
handle_sketch_rectangle.schema = {
    "type": "object",
    "properties": {
        "plane":  {"type": "string", "enum": ["XY", "XZ", "YZ"], "description": "草绘平面"},
        "length": {"type": "number", "minimum": 0.001, "description": "矩形长度 (mm)"},
        "width":  {"type": "number", "minimum": 0.001, "description": "矩形宽度 (mm)"},
        "x":      {"type": "number", "description": "中心 X 坐标 (mm), 默认 0"},
        "y":      {"type": "number", "description": "中心 Y 坐标 (mm), 默认 0"},
    },
    "required": ["plane", "length", "width"],
}
```

```python
# operations/features.py — 特征操作

def handle_pad_extrude(fc, params):
    """
    将草图拉伸为实体。

    Parameters:
        sketch_seq (int): 引用的草图序号
        height (float): 拉伸高度 (mm)
        direction (str, optional): 拉伸方向 "Z" | "-Z"，默认 "Z"
        taper_angle (float, optional): 拔模角度 (度)，默认 0

    Returns:
        dict: { "feature_id": int, "status": "ok" }
    """
    sketch = fc.get_sketch_by_seq(params["sketch_seq"])
    if sketch is None:
        raise ValueError(f"Sketch with seq {params['sketch_seq']} not found")

    pad = fc.doc.addObject("PartDesign::Pad", f"Pad_{fc.feature_count}")
    pad.Profile = sketch
    pad.Length = float(params["height"])
    pad.Length2 = 0.0
    pad.Reversed = 1 if params.get("direction") == "-Z" else 0
    pad.TaperAngle = float(params.get("taper_angle", 0))

    fc.body.addObject(pad)
    fc.feature_count += 1
    fc.recompute()
    return {"feature_id": fc.feature_count - 1, "status": "ok"}
```

---

## 5. 工具契约

### 5.1 契约原则

1. **每个工具只做一件事**（原子操作）
2. **参数名使用蛇形命名**
3. **长度单位统一为 mm**
4. **角度单位统一为度**
5. **返回值必须是 JSON 对象字符串**
6. **错误时必须抛出异常，Server 捕获后返回 MCP error**

### 5.2 工具列表

| 工具名 | 用途 | 必需参数 | 可选参数 |
|--------|------|----------|----------|
| `sketch_rectangle` | 矩形草图 | plane, length, width | x, y |
| `sketch_circle` | 圆形草图 | plane, radius | center_x, center_y |
| `sketch_polygon` | 多边形草图 | plane, sides, radius | center_x, center_y |
| `sketch_line` | 线段草图 | plane, x1, y1, x2, y2 | — |
| `pad_extrude` | 拉伸凸台 | sketch_seq, height | direction, taper_angle, reversed |
| `pocket_circular` | 圆形开孔 | sketch_seq | depth, through, reversed |
| `pocket_rectangular` | 矩形开孔 | sketch_seq | depth, through |
| `revolution` | 旋转特征 | sketch_seq, angle | axis |
| `groove` | 沟槽 | sketch_seq, angle | axis |
| `fillet` | 倒圆角 | edge_ids, radius | — |
| `chamfer` | 倒角 | edge_ids, size | — |
| `mirror` | 镜像 | feature_seq, plane | — |
| `linear_pattern` | 线性阵列 | feature_seq, direction, count, spacing | — |
| `circular_pattern` | 圆周阵列 | feature_seq, center, count, angle | — |
| `datum_plane` | 创建基准面 | offset, base_plane | — |
| `export_stl` | 导出 STL | file_path | body_name |
| `export_step` | 导出 STEP | file_path | body_name |
| `save_fcstd` | 保存项目 | file_path | — |

### 5.3 返回值规范

```json
// 成功
{"sketch_id": 3, "status": "ok"}

// 成功（带附加信息）
{"feature_id": 5, "status": "ok", "message": "Pad created with taper angle 5°"}

// 失败（通过 MCP error code -32000）
{"detail": "Sketch with seq 10 not found", "hint": "请检查 sketch_seq 参数"}
```

---

## 6. FreeCAD 文档生命周期

### 6.1 文档状态机

```
       ┌──────────┐
       │  初始     │  Start()
       │ (no doc) │
       └────┬─────┘
            │ initialize
            ▼
       ┌──────────┐
       │  激活     │  Document + Body 已创建
       │ (active) │
       └────┬─────┘
            │ tools/call (多次)
            ▼
       ┌──────────┐
       │  修改中   │  多次 CAD 操作
       │ (dirty)  │
       └────┬─────┘
            │ save_fcstd / shutdown
            ▼
       ┌──────────┐
       │  已保存   │  文档持久化到磁盘
       │ (saved)  │
       └──────────┘
```

### 6.2 跨会话恢复

```python
def load_from_fcstd(self, file_path: str):
    """从 .FCStd 文件恢复文档状态。"""
    if os.path.exists(file_path):
        FreeCAD.openDocument(file_path)
        self.doc = FreeCAD.ActiveDocument
        self.body = self.doc.getObject("Body")
        # 扫描现有特征，更新 feature_count
        self.feature_count = len(self.body.Objects)
```

---

## 7. 容错与恢复

### 7.1 操作失败处理链

```
MCP Server 执行操作
    │
    ├─ 成功 → 返回结果 → 命令栈标记 StatusExecuted
    │
    └─ 失败 → 返回错误 → LLM 收到错误信息
         │
         ├─ LLM 决定重试（修改参数）
         │
         └─ LLM 决定跳过 → 命令栈标记 StatusFailed
              │
              └─ 继续下一步
```

### 7.2 Python Server 的自我保护

```python
# freecad_manager.py 中的保护措施

import signal

class FreeCADManager:
    def __init__(self):
        self.doc = None
        self._operation_timeout = 30  # 单次操作超时 30 秒

    def _with_timeout(self, func, timeout=None):
        """带超时的操作执行（防止 FreeCAD 卡死）。"""
        import threading

        result = []
        error = []

        def worker():
            try:
                result.append(func())
            except Exception as e:
                error.append(e)

        t = threading.Thread(target=worker, daemon=True)
        t.start()
        t.join(timeout or self._operation_timeout)

        if t.is_alive():
            raise TimeoutError("FreeCAD operation timed out")
        if error:
            raise error[0]
        return result[0]

    def safe_recompute(self):
        """安全的 recompute，带超时和异常捕获。"""
        try:
            self._with_timeout(self.doc.recompute)
        except Exception as e:
            # 记录错误但不崩溃
            print(f"Warning: recompute failed: {e}", file=sys.stderr)
            # 尝试恢复：重新初始化
            self.initialize()
```

---

## 8. 环境要求

### 8.1 前提条件

| 依赖 | 版本要求 | 说明 |
|------|----------|------|
| FreeCAD | ≥ 0.21 | headless 模式（`FreeCADCmd`） |
| Python | ≥ 3.9 | FreeCAD 内置 Python 3 |
| 无额外 Python 包 | — | 使用标准库 + FreeCAD Python API |

### 8.2 启动方式

```bash
# 方式 1：标准 FreeCAD Python 解释器
FreeCADCmd -c freecad/server/server.py

# 方式 2：如果 FreeCAD 已添加 Python 路径
python3 -m freecad_mcp_server

# 方式 3：通过 Seelex 自动启动（Go 端 exec）
# Go 端配置 FreeCADCmd 路径（在 seele.yaml 中）
```

### 8.3 seele.yaml 配置扩展

```yaml
# seele.yaml — CAD 相关配置
cad:
  freecad:
    executable: "FreeCADCmd"           # FreeCAD headless 可执行文件路径
    server_script: "freecad/server/server.py"
    startup_timeout: 30s               # 启动超时
    operation_timeout: 120s            # 单次操作超时
    auto_save: true                    # 自动保存 .FCStd
    save_dir: ".seelex/cad/"           # 保存目录
```

---

## 9. 完整文件骨架

```
freecad/                          # Go 端
├── freecad.go                    # 包声明 + 操作常量
├── ops.go                        # 参数类型定义
├── ops_test.go                   # 参数序列化测试
├── schema.go                     # AllSchemas 注册表
├── validate.go                   # Validate 参数验证
├── validate_test.go              # 验证测试
│
└── server/                       # Python 端
    ├── server.py                 # MCP Server 主入口
    ├── mcp_protocol.py           # JSON-RPC 协议层
    ├── freecad_manager.py        # FreeCAD 文档管理
    ├── operations/
    │   ├── __init__.py
    │   ├── sketches.py           # 草图操作
    │   ├── features.py           # 特征操作
    │   ├── modifiers.py          # 修饰操作
    │   └── io.py                 # 导入导出
    ├── utils.py                  # 工具函数
    └── test/
        ├── test_sketches.py
        ├── test_features.py
        └── test_server.py
```
