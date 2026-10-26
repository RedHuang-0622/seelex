package adapters

import (
	"reflect"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	sessionaccess "github.com/RedHuang-0622/seelex/middle/view/sessionaccess"
)

func TestSessionRecordsPortMatchesMiddlePaginator(t *testing.T) {
	records := []dto.ConversationRecord{
		{ID: "u1", Kind: dto.ConversationRecordUser, Content: "hello"},
		{ID: "a1", Kind: dto.ConversationRecordAssistant, Content: "world"},
	}
	request := dto.ConversationViewRequest{Offset: 1, Limit: 1}
	got := NewSessionRecordsPort().PaginateRecords("session-a", records, request)
	want := sessionaccess.PaginateRecords("session-a", records, request)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paginator mismatch:\ngot  %+v\nwant %+v", got, want)
	}
}
