package freecad

import (
	"encoding/json"
	"fmt"
)

// ── Schema types ────────────────────────────────────────────────

// OpSchema describes a single CAD operation's metadata and parameter schema.
type OpSchema struct {
	Op          string             `json:"op"`           // Operation name
	Description string             `json:"description"`  // Human-readable description
	ParamSchema *json.RawMessage   `json:"param_schema"` // JSON Schema for parameters
}

// AllSchemas is the complete registry of supported CAD operations.
// Each entry defines the operation name, description, and parameter schema.
var AllSchemas = []OpSchema{
	// ── Sketch operations ──
	{
		Op: OpSketchRectangle, Description: "在指定平面上创建矩形草图",
		ParamSchema: rawSchema(SketchRectangleParams{}),
	},
	{
		Op: OpSketchCircle, Description: "在指定平面上创建圆形草图",
		ParamSchema: rawSchema(SketchCircleParams{}),
	},
	{
		Op: OpSketchLine, Description: "在草图上创建线段",
		ParamSchema: rawSchema(SketchLineParams{}),
	},
	{
		Op: OpSketchConstraint, Description: "应用几何约束到草图元素",
		ParamSchema: rawSchema(struct {
			Type  string `json:"type"`
			Elems []int  `json:"elems"`
		}{}),
	},

	// ── 3D Feature operations ──
	{
		Op: OpPadExtrude, Description: "将草图拉伸为实体凸台",
		ParamSchema: rawSchema(PadExtrudeParams{}),
	},
	{
		Op: OpPocketCircular, Description: "基于圆形草图的圆形开孔",
		ParamSchema: rawSchema(PocketCircularParams{}),
	},
	{
		Op: OpPocketRectangular, Description: "基于矩形草图的矩形开孔",
		ParamSchema: rawSchema(PocketRectangularParams{}),
	},
	{
		Op: OpRevolution, Description: "围绕轴旋转草图创建旋转体",
		ParamSchema: rawSchema(RevolutionParams{}),
	},

	// ── Modify operations ──
	{
		Op: OpFillet, Description: "为边添加圆角",
		ParamSchema: rawSchema(FilletParams{}),
	},
	{
		Op: OpChamfer, Description: "为边添加倒角",
		ParamSchema: rawSchema(ChamferParams{}),
	},
	{
		Op: OpMirror, Description: "镜像特征",
		ParamSchema: rawSchema(MirrorParams{}),
	},

	// ── File I/O operations ──
	{
		Op: OpExportSTL, Description: "导出为 STL 格式",
		ParamSchema: rawSchema(ExportSTLParams{}),
	},
	{
		Op: OpExportSTEP, Description: "导出为 STEP 格式",
		ParamSchema: rawSchema(ExportSTEPParams{}),
	},
	{
		Op: OpSaveFCStd, Description: "保存为 FreeCAD .FCStd 格式",
		ParamSchema: rawSchema(SaveFCStdParams{}),
	},
}

// SchemaByOp returns the OpSchema for a given operation name.
func SchemaByOp(op string) (OpSchema, bool) {
	for _, s := range AllSchemas {
		if s.Op == op {
			return s, true
		}
	}
	return OpSchema{}, false
}

// rawSchema serializes a Go struct to a JSON RawMessage for schema storage.
// Panics on failure (used for compile-time-safe schema initialization).
func rawSchema(v interface{}) *json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("freecad: schema marshal failed for %T: %v", v, err))
	}
	raw := json.RawMessage(data)
	return &raw
}
