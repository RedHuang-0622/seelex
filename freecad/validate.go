package freecad

import (
	"encoding/json"
	"fmt"
)

// Validate parses and validates operation parameters against the expected schema.
//
// Parameters:
//   - op: operation name (must match one of the Op* constants)
//   - params: raw JSON parameters
//
// Returns:
//   - The parsed and validated parameter struct (typed)
//   - An error if validation fails
//
// Validation rules (common across all ops):
//   - Required fields must be present and non-zero
//   - Numeric fields must be within allowed ranges
//   - String fields must match allowed values (e.g., plane names)
func Validate(op string, params json.RawMessage) (interface{}, error) {
	switch op {
	case OpSketchRectangle:
		return validateSketchRectangle(params)
	case OpSketchCircle:
		return validateSketchCircle(params)
	case OpSketchLine:
		return validateSketchLine(params)
	case OpPadExtrude:
		return validatePadExtrude(params)
	case OpPocketCircular:
		return validatePocketCircular(params)
	case OpPocketRectangular:
		return validatePocketRectangular(params)
	case OpRevolution:
		return validateRevolution(params)
	case OpFillet:
		return validateFillet(params)
	case OpChamfer:
		return validateChamfer(params)
	case OpMirror:
		return validateMirror(params)
	case OpExportSTL:
		return validateExportSTL(params)
	case OpExportSTEP:
		return validateExportSTEP(params)
	case OpSaveFCStd:
		return validateSaveFCStd(params)
	default:
		return nil, fmt.Errorf("freecad: unknown operation %q", op)
	}
}

// ── Individual validators ──────────────────────────────────────

func validateSketchRectangle(params json.RawMessage) (*SketchRectangleParams, error) {
	var p SketchRectangleParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("sketch_rectangle: %w", err)
	}
	if err := validatePlane(p.Plane); err != nil {
		return nil, fmt.Errorf("sketch_rectangle: %w", err)
	}
	if p.Length <= 0 {
		return nil, fmt.Errorf("sketch_rectangle: length must be positive, got %f", p.Length)
	}
	if p.Width <= 0 {
		return nil, fmt.Errorf("sketch_rectangle: width must be positive, got %f", p.Width)
	}
	return &p, nil
}

func validateSketchCircle(params json.RawMessage) (*SketchCircleParams, error) {
	var p SketchCircleParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("sketch_circle: %w", err)
	}
	if err := validatePlane(p.Plane); err != nil {
		return nil, fmt.Errorf("sketch_circle: %w", err)
	}
	if p.Radius <= 0 {
		return nil, fmt.Errorf("sketch_circle: radius must be positive, got %f", p.Radius)
	}
	return &p, nil
}

func validateSketchLine(params json.RawMessage) (*SketchLineParams, error) {
	var p SketchLineParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("sketch_line: %w", err)
	}
	if err := validatePlane(p.Plane); err != nil {
		return nil, fmt.Errorf("sketch_line: %w", err)
	}
	return &p, nil
}

func validatePadExtrude(params json.RawMessage) (*PadExtrudeParams, error) {
	var p PadExtrudeParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("pad_extrude: %w", err)
	}
	if p.SketchSeq <= 0 {
		return nil, fmt.Errorf("pad_extrude: sketch_seq must be positive, got %d", p.SketchSeq)
	}
	if p.Height <= 0 {
		return nil, fmt.Errorf("pad_extrude: height must be positive, got %f", p.Height)
	}
	if p.Direction != "" && p.Direction != "Z" && p.Direction != "-Z" {
		return nil, fmt.Errorf("pad_extrude: invalid direction %q, use \"Z\" or \"-Z\"", p.Direction)
	}
	return &p, nil
}

func validatePocketCircular(params json.RawMessage) (*PocketCircularParams, error) {
	var p PocketCircularParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("pocket_circular: %w", err)
	}
	if p.SketchSeq <= 0 {
		return nil, fmt.Errorf("pocket_circular: sketch_seq must be positive, got %d", p.SketchSeq)
	}
	if !p.Through && p.Depth <= 0 {
		return nil, fmt.Errorf("pocket_circular: depth must be positive when not through, got %f", p.Depth)
	}
	return &p, nil
}

func validatePocketRectangular(params json.RawMessage) (*PocketRectangularParams, error) {
	var p PocketRectangularParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("pocket_rectangular: %w", err)
	}
	if p.SketchSeq <= 0 {
		return nil, fmt.Errorf("pocket_rectangular: sketch_seq must be positive, got %d", p.SketchSeq)
	}
	return &p, nil
}

func validateRevolution(params json.RawMessage) (*RevolutionParams, error) {
	var p RevolutionParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("revolution: %w", err)
	}
	if p.SketchSeq <= 0 {
		return nil, fmt.Errorf("revolution: sketch_seq must be positive, got %d", p.SketchSeq)
	}
	if p.Angle <= 0 || p.Angle > 360 {
		return nil, fmt.Errorf("revolution: angle must be in (0, 360], got %f", p.Angle)
	}
	return &p, nil
}

func validateFillet(params json.RawMessage) (*FilletParams, error) {
	var p FilletParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("fillet: %w", err)
	}
	if len(p.EdgeIDs) == 0 {
		return nil, fmt.Errorf("fillet: at least one edge_id required")
	}
	if p.Radius <= 0 {
		return nil, fmt.Errorf("fillet: radius must be positive, got %f", p.Radius)
	}
	return &p, nil
}

func validateChamfer(params json.RawMessage) (*ChamferParams, error) {
	var p ChamferParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("chamfer: %w", err)
	}
	if len(p.EdgeIDs) == 0 {
		return nil, fmt.Errorf("chamfer: at least one edge_id required")
	}
	if p.Size <= 0 {
		return nil, fmt.Errorf("chamfer: size must be positive, got %f", p.Size)
	}
	return &p, nil
}

func validateMirror(params json.RawMessage) (*MirrorParams, error) {
	var p MirrorParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("mirror: %w", err)
	}
	if p.FeatureSeq <= 0 {
		return nil, fmt.Errorf("mirror: feature_seq must be positive, got %d", p.FeatureSeq)
	}
	if p.Plane == "" {
		return nil, fmt.Errorf("mirror: plane is required")
	}
	return &p, nil
}

func validateExportSTL(params json.RawMessage) (*ExportSTLParams, error) {
	var p ExportSTLParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("export_stl: %w", err)
	}
	if p.FilePath == "" {
		return nil, fmt.Errorf("export_stl: file_path is required")
	}
	return &p, nil
}

func validateExportSTEP(params json.RawMessage) (*ExportSTEPParams, error) {
	var p ExportSTEPParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("export_step: %w", err)
	}
	if p.FilePath == "" {
		return nil, fmt.Errorf("export_step: file_path is required")
	}
	return &p, nil
}

func validateSaveFCStd(params json.RawMessage) (*SaveFCStdParams, error) {
	var p SaveFCStdParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("save_fcstd: %w", err)
	}
	if p.FilePath == "" {
		return nil, fmt.Errorf("save_fcstd: file_path is required")
	}
	return &p, nil
}

// ── Shared validators ──────────────────────────────────────────

func validatePlane(plane string) error {
	switch plane {
	case "XY", "XZ", "YZ":
		return nil
	default:
		return fmt.Errorf("invalid plane %q, must be XY, XZ, or YZ", plane)
	}
}
