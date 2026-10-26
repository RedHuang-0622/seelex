package review

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

type registrationProbe struct {
	name        string
	description string
	schema      map[string]interface{}
	handler     func(context.Context, string) (string, error)
	calls       int
}

func (probe *registrationProbe) RegisterTool(
	name, description string,
	schema map[string]interface{},
	handler func(context.Context, string) (string, error),
) {
	probe.calls++
	probe.name = name
	probe.description = description
	probe.schema = schema
	probe.handler = handler
}

type registrationPort struct {
	calls int
	files []dto.ReviewFileRef
}

func (port *registrationPort) RequestFileReview(_ context.Context, files []dto.ReviewFileRef) (dto.ReviewRequest, error) {
	port.calls++
	port.files = files
	return dto.ReviewRequest{RequestID: "review-1", Files: files}, nil
}

func TestRegisterShowFilesToolUsesTheCanonicalShapeOnce(t *testing.T) {
	registrar := &registrationProbe{}
	port := &registrationPort{}

	RegisterShowFilesTool(registrar, port)

	if registrar.calls != 1 {
		t.Fatalf("RegisterTool calls = %d, want 1", registrar.calls)
	}
	if registrar.name != ShowFilesToolName || registrar.description != ShowFilesDescription() {
		t.Fatalf("registered metadata = (%q, %q)", registrar.name, registrar.description)
	}
	if registrar.schema == nil || registrar.handler == nil {
		t.Fatalf("registered schema/handler missing: schema nil=%v handler nil=%v", registrar.schema == nil, registrar.handler == nil)
	}
	if _, err := registrar.handler(context.Background(), `{"files":[{"path":"docs/a.md"}]}`); err != nil {
		t.Fatalf("registered handler failed: %v", err)
	}
	if port.calls != 1 || len(port.files) != 1 || port.files[0].Path != "docs/a.md" {
		t.Fatalf("handler did not reach the canonical port: calls=%d files=%+v", port.calls, port.files)
	}
}
