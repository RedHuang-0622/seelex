package adapters

import (
	"errors"
	"reflect"
	"testing"

	viewerrors "github.com/RedHuang-0622/seelex/application/errorpresentation"
	"github.com/RedHuang-0622/seelex/contract"
)

func TestErrorPresentationPortMatchesMiddleRules(t *testing.T) {
	port := NewErrorPresentationPort()
	err := errors.New("tool failed")

	gotText := port.PresentToolError("write_file", err)
	wantText := viewerrors.PresentToolError("write_file", err)
	if gotText != wantText {
		t.Fatalf("tool error text mismatch:\ngot  %q\nwant %q", gotText, wantText)
	}

	gotWrapped := port.Wrap(err, contract.ErrorCodeReActBudget)
	wantWrapped := viewerrors.Wrap(err, contract.ErrorCodeReActBudget)
	if reflect.TypeOf(gotWrapped) != reflect.TypeOf(wantWrapped) ||
		gotWrapped.Error() != wantWrapped.Error() ||
		viewerrors.StructuredCode(gotWrapped) != viewerrors.StructuredCode(wantWrapped) {
		t.Fatalf("wrapped error mismatch:\ngot  %#v\nwant %#v", gotWrapped, wantWrapped)
	}
}
