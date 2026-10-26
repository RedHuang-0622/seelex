package session

import (
	"testing"

	"github.com/RedHuang-0622/seelex/contract/protocol"
	switching "github.com/RedHuang-0622/seelex/kernel/sessionpolicy"
)

func TestMetaApplicationDelegatesRead(t *testing.T) {
	application := NewMetaApplication(MetaDeps{
		Meta: func() switching.MetaDeps { return switching.MetaDeps{} },
		ReadMeta: func(sessionID string) (model.SessionMeta, error) {
			return model.SessionMeta{Alias: sessionID, Pinned: true, SortOrder: 2}, nil
		},
	})

	meta, err := application.SessionMeta("session-a")
	if err != nil || meta.Alias != "session-a" || !meta.Pinned || meta.SortOrder != 2 {
		t.Fatalf("SessionMeta = %+v, %v", meta, err)
	}
}

func TestNilMetaApplicationPreservesUnsupportedReadSemantics(t *testing.T) {
	var application *MetaApplication
	if _, err := application.SessionMeta("session-a"); err == nil {
		t.Fatal("nil metadata application should preserve unsupported read error")
	}
}
