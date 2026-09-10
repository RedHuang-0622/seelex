package adapters

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/model"
)

// TestTranscriptRoleFieldsRoundTrip 钉住 R2/R4 字段不会在 model ↔
// sessionstore 适配层被擦除。
func TestTranscriptRoleFieldsRoundTrip(t *testing.T) {
	events := []model.TranscriptEvent{{
		Seq: 7, Role: "assistant", Kind: model.TranscriptEventKindLLM,
		Content: "tl reply", RoleName: "tl", RoleSessionID: "tl-1",
		RoundID: 2, UnitSeq: 3, WireMaterial: true,
	}}
	stored := storeTranscriptEvents(events)
	if len(stored) != 1 {
		t.Fatalf("stored events = %d", len(stored))
	}
	if stored[0].RoleName != "tl" || stored[0].RoleSessionID != "tl-1" ||
		stored[0].RoundID != 2 || stored[0].UnitSeq != 3 || !stored[0].WireMaterial {
		t.Fatalf("stored role fields lost: %+v", stored[0])
	}
	back := adaptTranscriptEvents(stored)
	if len(back) != 1 || back[0].RoleName != "tl" || back[0].RoleSessionID != "tl-1" ||
		back[0].RoundID != 2 || back[0].UnitSeq != 3 || !back[0].WireMaterial {
		t.Fatalf("adapted role fields lost: %+v", back)
	}
}
