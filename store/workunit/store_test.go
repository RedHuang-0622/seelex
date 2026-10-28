package workunit

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestStoreRoundTripAndPhysicalPath(t *testing.T) {
	root := t.TempDir()
	store := New(root)
	record := Record{
		NodeID: "node-a", SessionID: "sub-a", MainSessionID: "main",
		Goal: "inspect", Status: "running",
	}
	if err := store.Write(record); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, DirName, FileName("main", "sub-a"))
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("record path %s: %v", path, err)
	}
	loaded, err := store.Read("main", "sub-a")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SchemaVersion != SchemaVersion || loaded.UpdatedAt.IsZero() {
		t.Fatalf("defaults missing: %+v", loaded)
	}
}

func TestStoreMissingListDeleteAndTerminal(t *testing.T) {
	store := New(t.TempDir())
	if records, err := store.List(); err != nil || records == nil || len(records) != 0 {
		t.Fatalf("missing list = %#v, %v", records, err)
	}
	if _, err := store.Read("main", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing read error = %v", err)
	}
	for _, id := range []string{"b", "a"} {
		if err := store.Write(Record{
			NodeID: id, SessionID: "sub-" + id, MainSessionID: "main",
			Goal: "original", Status: "running",
		}); err != nil {
			t.Fatal(err)
		}
	}
	records, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{records[0].NodeID, records[1].NodeID}; !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("list order = %v", got)
	}
	if err := store.MarkTerminal("main", Record{
		SessionID: "sub-a", NodeID: "forged", Goal: "forged",
		Status: "failed", Error: "failed", EndedAt: time.Unix(10, 0),
	}); err != nil {
		t.Fatal(err)
	}
	terminal, err := store.Read("main", "sub-a")
	if err != nil {
		t.Fatal(err)
	}
	if terminal.NodeID != "a" || terminal.Goal != "original" || terminal.Status != "failed" {
		t.Fatalf("terminal record changed identity: %+v", terminal)
	}
	if err := store.Delete("main", "sub-a"); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("main", "sub-a"); err != nil {
		t.Fatalf("delete must be idempotent: %v", err)
	}
}

type recordingRecorder struct {
	record Record
}

func (r *recordingRecorder) Save(_, _ string, record Record) error {
	r.record = record
	return nil
}
func (*recordingRecorder) ProjectID() string { return "project" }
func (*recordingRecorder) Load(string, string, string) (Record, error) {
	return Record{}, nil
}
func (*recordingRecorder) List(string, string) ([]Record, error) { return nil, nil }
func (*recordingRecorder) Delete(string, string, string) error   { return nil }

func TestWriterChainOnlyFillsOwnFacts(t *testing.T) {
	recorder := &recordingRecorder{}
	writer := NewWriter(recorder).With(func(record Record) Record {
		record.Unit.Kind = "teammate"
		return record
	}).With(func(record Record) Record {
		if record.Unit.Kind == "" {
			record.Unit.Kind = "subagent"
		}
		return record
	})
	if err := writer.Save("project", "main", Record{SessionID: "sub"}); err != nil {
		t.Fatal(err)
	}
	if recorder.record.Unit.Kind != "teammate" {
		t.Fatalf("chain must preserve outer identity: %+v", recorder.record)
	}
	if writer.Store() != recorder {
		t.Fatal("writer lost recorder read face")
	}
	if err := (*Writer)(nil).Save("project", "main", Record{}); err == nil {
		t.Fatal("nil writer must fail loudly")
	}
}
