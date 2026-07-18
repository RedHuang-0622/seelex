// Package freecad provides CAD operation schemas and a validation middleware
// for FreeCAD MCP calls. Unlike WebSearch which accepts free-form queries,
// CAD operations have strict dimensional constraints that must be validated
// BEFORE reaching the MCP server.
//
// Architecture:
//
//	Seele framework (MCP lifecycle, circuit breaker, dispatch)
//	     │
//	     ├── mcpstack (trace ALL MCP calls — generic middleware)
//	     │
//	     └── freecad.Validator (pre-flight param check for CAD — domain-specific)
//
// The actual FreeCAD execution is delegated to an existing MCP server
// (see plugins/freecad/plugin.md). This package is a validation layer only.
package freecad

// ── CAD operation type constants ───────────────────────────────
// These match the tool names exposed by the FreeCAD MCP server.
const (
	// Sketch operations
	OpSketchRectangle   = "sketch_rectangle"
	OpSketchCircle      = "sketch_circle"
	OpSketchPolygon     = "sketch_polygon"
	OpSketchLine        = "sketch_line"
	OpSketchArc         = "sketch_arc"
	OpSketchConstraint  = "sketch_constraint"

	// 3D Feature operations
	OpPadExtrude        = "pad_extrude"
	OpPocketCircular    = "pocket_circular"
	OpPocketRectangular = "pocket_rectangular"
	OpRevolution        = "revolution"
	OpGroove            = "groove"

	// Modify operations
	OpFillet            = "fillet"
	OpChamfer           = "chamfer"
	OpMirror            = "mirror"
	OpLinearPattern     = "linear_pattern"
	OpCircularPattern   = "circular_pattern"

	// Reference geometry
	OpDatumPlane        = "datum_plane"
	OpDatumLine         = "datum_line"

	// File IO
	OpExportSTL         = "export_stl"
	OpExportSTEP        = "export_step"
	OpSaveFCStd         = "save_fcstd"
)

// AllCADOps returns all registered CAD operation names.
func AllCADOps() []string {
	return []string{
		OpSketchRectangle, OpSketchCircle, OpSketchPolygon, OpSketchLine, OpSketchArc,
		OpSketchConstraint,
		OpPadExtrude, OpPocketCircular, OpPocketRectangular, OpRevolution, OpGroove,
		OpFillet, OpChamfer, OpMirror, OpLinearPattern, OpCircularPattern,
		OpDatumPlane, OpDatumLine,
		OpExportSTL, OpExportSTEP, OpSaveFCStd,
	}
}
