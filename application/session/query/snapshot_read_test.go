package query

import (
	"errors"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func TestReadSnapshotChoosesColdAndResidentPaths(t *testing.T) {
	cold := model.SessionSnapshot{Revision: 1}
	resident := model.SessionSnapshot{Revision: 2}
	calls := []string{}
	deps := SnapshotReadDeps{
		State: func(string) SnapshotReadState { return SnapshotReadState{Resident: true, ViewSessionID: "s1"} },
		ColdSnapshot: func(string) (model.SessionSnapshot, error) {
			calls = append(calls, "cold")
			return cold, nil
		},
		ResidentSnapshot: func(string) (model.SessionSnapshot, error) {
			calls = append(calls, "resident")
			return resident, nil
		},
	}
	if got, err := ReadSnapshot("s1", deps); err != nil || got.Revision != 2 {
		t.Fatalf("resident snapshot = %+v err=%v", got, err)
	}
	deps.State = func(string) SnapshotReadState { return SnapshotReadState{Resident: false} }
	if got, err := ReadSnapshot("s2", deps); err != nil || got.Revision != 1 {
		t.Fatalf("cold snapshot = %+v err=%v", got, err)
	}
	if len(calls) != 2 || calls[0] != "resident" || calls[1] != "cold" {
		t.Fatalf("calls = %v", calls)
	}
}

func TestReadSnapshotFallsBackWhenColdContentReadFails(t *testing.T) {
	resident := model.SessionSnapshot{Revision: 9}
	deps := SnapshotReadDeps{
		State: func(string) SnapshotReadState {
			return SnapshotReadState{Resident: true, ViewSessionID: "view", ContentUnloaded: true}
		},
		ColdSnapshot: func(string) (model.SessionSnapshot, error) {
			return model.SessionSnapshot{}, errors.New("disk unavailable")
		},
		ResidentSnapshot: func(string) (model.SessionSnapshot, error) {
			return resident, nil
		},
	}
	if got, err := ReadSnapshot("other", deps); err != nil || got.Revision != 9 {
		t.Fatalf("fallback snapshot = %+v err=%v", got, err)
	}
}

func TestReadSnapshotRejectsEmptyID(t *testing.T) {
	if _, err := ReadSnapshot("  ", SnapshotReadDeps{}); err == nil {
		t.Fatal("empty session ID must fail")
	}
}
