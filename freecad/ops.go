package freecad

// ── Parameter type definitions ──────────────────────────────────
//
// These types define the JSON Schema-compatible parameter structures for
// each CAD operation. They are used for:
//   1. Schema registration via AllSchemas
//   2. Parameter validation via Validate()
//   3. Auto-generating LLM tool definitions
//
// All length units: millimeters (mm)
// All angle units: degrees (°)

// ── Sketch operations ──────────────────────────────────────────

// SketchRectangleParams creates a rectangle on a sketch plane.
type SketchRectangleParams struct {
	Plane  string  `json:"plane"`             // "XY" | "XZ" | "YZ" or custom plane name
	Length float64 `json:"length"`            // Rectangle length (mm), must be > 0
	Width  float64 `json:"width"`             // Rectangle width (mm), must be > 0
	X      float64 `json:"x,omitempty"`       // Center X (mm), default 0
	Y      float64 `json:"y,omitempty"`       // Center Y (mm), default 0
}

// SketchCircleParams creates a circle on a sketch plane.
type SketchCircleParams struct {
	Plane    string  `json:"plane"`              // "XY" | "XZ" | "YZ" or custom plane name
	Radius   float64 `json:"radius"`             // Circle radius (mm), must be > 0
	CenterX  float64 `json:"center_x,omitempty"` // Center X (mm), default 0
	CenterY  float64 `json:"center_y,omitempty"` // Center Y (mm), default 0
}

// SketchLineParams creates a line segment between two points.
type SketchLineParams struct {
	Plane string  `json:"plane"`  // Sketch plane
	X1    float64 `json:"x1"`     // Start point X
	Y1    float64 `json:"y1"`     // Start point Y
	X2    float64 `json:"x2"`     // End point X
	Y2    float64 `json:"y2"`     // End point Y
}

// ── 3D Feature operations ──────────────────────────────────────

// PadExtrudeParams extrudes a sketch into a solid body.
type PadExtrudeParams struct {
	SketchSeq  int     `json:"sketch_seq"`            // Referenced sketch sequence number
	Height     float64 `json:"height"`                // Extrusion height (mm), must be > 0
	Direction  string  `json:"direction,omitempty"`   // "Z" (default) | "-Z" | "X" | "-X"
	Reversed   bool    `json:"reversed,omitempty"`    // Reverse extrusion direction
	TaperAngle float64 `json:"taper_angle,omitempty"` // Draft angle (degrees), default 0
}

// PocketCircularParams creates a circular cut through or into the body.
type PocketCircularParams struct {
	SketchSeq int     `json:"sketch_seq"`            // Referenced sketch containing a circle
	Depth     float64 `json:"depth,omitempty"`       // Pocket depth (mm), ignored if Through=true
	Through   bool    `json:"through,omitempty"`     // Cut through entire body
	Reversed  bool    `json:"reversed,omitempty"`    // Reverse cut direction
}

// PocketRectangularParams creates a rectangular pocket cut.
type PocketRectangularParams struct {
	SketchSeq int     `json:"sketch_seq"`
	Depth     float64 `json:"depth,omitempty"`
	Through   bool    `json:"through,omitempty"`
	Reversed  bool    `json:"reversed,omitempty"`
}

// RevolutionParams creates a revolved feature from a sketch around an axis.
type RevolutionParams struct {
	SketchSeq int     `json:"sketch_seq"`
	Angle     float64 `json:"angle"`               // Revolution angle (degrees), 360 for full
	Axis      string  `json:"axis,omitempty"`      // "X" | "Y" | "Z" or custom axis ref
	Reversed  bool    `json:"reversed,omitempty"`
}

// ── Modify operations ──────────────────────────────────────────

// FilletParams rounds edges with a specified radius.
type FilletParams struct {
	EdgeIDs []int   `json:"edge_ids"`       // Edge identifier(s), required
	Radius  float64 `json:"radius"`         // Fillet radius (mm), must be > 0
}

// ChamferParams bevels edges with a specified size.
type ChamferParams struct {
	EdgeIDs []int   `json:"edge_ids"`       // Edge identifier(s), required
	Size    float64 `json:"size"`           // Chamfer size (mm), must be > 0
}

// MirrorParams mirrors a feature across a plane.
type MirrorParams struct {
	FeatureSeq int    `json:"feature_seq"`         // Feature to mirror
	Plane      string `json:"plane"`               // Mirror plane
}

// ── File I/O operations ────────────────────────────────────────

// ExportSTLParams exports the design to STL format.
type ExportSTLParams struct {
	FilePath string `json:"file_path"`              // Output file path
	BodyName string `json:"body_name,omitempty"`    // Specific body name (default: entire doc)
}

// ExportSTEPParams exports to STEP format.
type ExportSTEPParams struct {
	FilePath string `json:"file_path"`
	BodyName string `json:"body_name,omitempty"`
}

// SaveFCStdParams saves the FreeCAD document to a .FCStd file.
type SaveFCStdParams struct {
	FilePath string `json:"file_path"`
}
