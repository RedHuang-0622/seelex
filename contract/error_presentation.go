package contract

// ErrorPresentationPort is the cross-layer error presentation seam used by
// chat and tool failures. Classification and user-facing wording remain owned
// by the Middle implementation; consumers only receive pure error/text values.
type ErrorPresentationPort interface {
	Wrap(err error, code string) error
	PresentToolError(toolName string, err error) string
}
