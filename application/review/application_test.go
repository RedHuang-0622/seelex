package review

import (
	"context"
	"testing"

	reviewtool "github.com/RedHuang-0622/seelex/backend/tool/review"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

type reviewFilePort struct{}

func testProcessorFactory(host Host) RequestProcessor {
	return reviewtool.NewRequests(host)
}

func (reviewFilePort) ReadFile(root, relPath string, limit int64) (dto.FileContent, error) {
	return dto.FileContent{
		Path: relPath, TextLike: true, Limit: limit, Base64: "eA==",
	}, nil
}

func (reviewFilePort) GitCommitFileContent(string, string, string, int64) (dto.FileContent, error) {
	return dto.FileContent{}, nil
}

func TestReviewApplicationPromotesAcceptedRequest(t *testing.T) {
	var setSession string
	var publishedSession string
	var requestedID string
	application := New(Deps{
		WorkspaceFile: func() (contract.WorkspaceFilePort, string, error) {
			return reviewFilePort{}, "/repo", nil
		},
		SessionIDFromContext: func(context.Context) string { return " session-a " },
		ViewSessionID:        func() string { return "view-a" },
		SetReviewRequest: func(sessionID string, request *dto.ReviewRequest) uint64 {
			setSession = sessionID
			requestedID = request.RequestID
			return 7
		},
		ClearReviewRequest: func(string) {},
		PublishSnapshotChanged: func(sessionID string, revision uint64) {
			publishedSession = sessionID
			if revision != 7 {
				t.Fatalf("revision = %d", revision)
			}
		},
		NewProcessor: testProcessorFactory,
	})
	request, err := application.RequestFileReview(context.Background(), []dto.ReviewFileRef{{Path: "docs/a.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Files) != 1 || request.RequestID == "" || requestedID != request.RequestID {
		t.Fatalf("request = %+v, setID=%q", request, requestedID)
	}
	if setSession != "session-a" || publishedSession != "session-a" {
		t.Fatalf("sessions = set:%q publish:%q", setSession, publishedSession)
	}
}

func TestReviewApplicationClearUsesViewSessionFallback(t *testing.T) {
	var cleared string
	application := New(Deps{
		ViewSessionID:      func() string { return "view-a" },
		ClearReviewRequest: func(sessionID string) { cleared = sessionID },
		NewProcessor:       testProcessorFactory,
	})
	application.ClearReviewRequest("  ")
	if cleared != "view-a" {
		t.Fatalf("cleared = %q", cleared)
	}
}

func TestReviewApplicationRequestIDIsUnique(t *testing.T) {
	application := New(Deps{})
	host := reviewHost{application: application}
	first := host.NextRequestID()
	second := host.NextRequestID()
	if first == second {
		t.Fatalf("request IDs collided: %q", first)
	}
}
