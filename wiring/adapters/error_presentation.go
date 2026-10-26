package adapters

import (
	viewerrors "github.com/RedHuang-0622/seelex/application/errorpresentation"
	"github.com/RedHuang-0622/seelex/contract"
)

type errorPresentationPort struct{}

var _ contract.ErrorPresentationPort = errorPresentationPort{}

// NewErrorPresentationPort exposes the Middle-owned error classification and
// tool-error wording without copying either rule into Application.
func NewErrorPresentationPort() contract.ErrorPresentationPort {
	return errorPresentationPort{}
}

func (errorPresentationPort) Wrap(err error, code string) error {
	return viewerrors.Wrap(err, code)
}

func (errorPresentationPort) PresentToolError(toolName string, err error) string {
	return viewerrors.PresentToolError(toolName, err)
}
