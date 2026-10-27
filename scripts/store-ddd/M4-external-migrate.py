#!/usr/bin/env python3
"""Apply the reviewed M4 application/adapter turn-commit migration."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import subprocess
import tempfile
from datetime import datetime, timezone
from pathlib import Path

APP_FILE = r'''package conversation

import (
	"context"
	"encoding/json"

	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
)

// TurnStoreCommitPort is the consumer-owned three-aggregate persistence port.
type TurnStoreCommitPort interface {
	WriteArtifacts(context.Context, string, string, []protocol.StoredToolResult) error
	WriteMessages(context.Context, string, string, []protocol.TranscriptEvent) error
	WriteCompactions(context.Context, string, string, []json.RawMessage) error
}

// TurnStoreCommitBatch is the aggregate batch for one durable turn.
type TurnStoreCommitBatch struct {
	ProjectID   string
	SessionID   string
	ToolResults []protocol.StoredToolResult
	Events      []protocol.TranscriptEvent
	Compactions []json.RawMessage
}

// TurnStoreCommitApplication owns the cross-aggregate turn write order.
type TurnStoreCommitApplication struct {
	port TurnStoreCommitPort
}

// NewTurnStoreCommit constructs the application turn unit-of-work.
func NewTurnStoreCommit(port TurnStoreCommitPort) *TurnStoreCommitApplication {
	return &TurnStoreCommitApplication{port: port}
}

// Commit preserves the M0 compatible artifact -> message -> compaction order.
func (application *TurnStoreCommitApplication) Commit(ctx context.Context, batch TurnStoreCommitBatch) error {
	if err := application.port.WriteArtifacts(ctx, batch.ProjectID, batch.SessionID, batch.ToolResults); err != nil {
		return err
	}
	if err := application.port.WriteMessages(ctx, batch.ProjectID, batch.SessionID, batch.Events); err != nil {
		return err
	}
	return application.port.WriteCompactions(ctx, batch.ProjectID, batch.SessionID, batch.Compactions)
}
'''

ADAPTER = r'''
type turnStoreCommitAdapter struct {
	writer *store.CommitWriter
}

func (adapter turnStoreCommitAdapter) WriteArtifacts(_ context.Context, projectID, sessionID string, results []protocol.StoredToolResult) error {
	return adapter.writer.WriteArtifacts(context.Background(), projectID, sessionID, storeToolResults(results))
}

func (adapter turnStoreCommitAdapter) WriteMessages(_ context.Context, projectID, sessionID string, events []protocol.TranscriptEvent) error {
	return adapter.writer.WriteMessages(context.Background(), projectID, sessionID, storeTranscriptEvents(events))
}

func (adapter turnStoreCommitAdapter) WriteCompactions(_ context.Context, projectID, sessionID string, records []json.RawMessage) error {
	return adapter.writer.WriteCompactions(context.Background(), projectID, sessionID, records)
}

func (port SessionPort) saveSessionTurnCommit(
	projectID, sessionID string,
	record model.SessionRecord,
	events []model.TranscriptEvent,
	results []model.StoredToolResult,
) error {
	commit := applicationconversation.NewTurnStoreCommit(turnStoreCommitAdapter{
		writer: store.NewCommitWriter(port.Manager.Router()),
	})
	return commit.Commit(context.Background(), applicationconversation.TurnStoreCommitBatch{
		ProjectID: projectID, SessionID: sessionID,
		ToolResults: results, Events: events, Compactions: compactionRecordPayloads(record),
	})
}
'''

SNAPSHOT = r'''func (port SessionPort) SaveSessionSnapshot(
	id string,
	providerHistory []contract.EngineMessage,
	record model.SessionRecord,
	events []model.TranscriptEvent,
	results []model.StoredToolResult,
) error {
	_ = providerHistory
	return port.saveSessionTurnCommit("", id, record, events, results)
}'''

WORKSPACE = r'''func (port SessionPort) SaveSessionSnapshotWorkspace(
	projectID, sessionID string,
	providerHistory []contract.EngineMessage,
	record model.SessionRecord,
	events []model.TranscriptEvent,
	results []model.StoredToolResult,
) error {
	_ = providerHistory
	if err := port.saveSessionTurnCommit(projectID, sessionID, record, events, results); err != nil {
		return err
	}
	if ref := record.ForkedFrom; ref != nil {
		if err := port.Manager.Router().ForkStacks(
			ref.ParentWorkspaceID, ref.ParentSessionID, projectID, sessionID, ref.ForkPoint.EventSeq,
		); err != nil {
			return fmt.Errorf("fork session stacks: %w", err)
		}
	}
	return nil
}'''


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def replace_between(text: str, start: str, end: str, replacement: str) -> str:
    left = text.find(start)
    right = text.find(end, left)
    if left < 0 or right < 0:
        raise ValueError(f"markers not found: {start!r}")
    return text[:left] + replacement + "\n\n" + text[right:]


def build_adapter(source: str) -> str:
    source = source.replace(
        'assemble "github.com/RedHuang-0622/seelex/backend/engine/assemble"',
        'applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"\n\tassemble "github.com/RedHuang-0622/seelex/backend/engine/assemble"',
        1,
    )
    source = source.replace(
        '"github.com/RedHuang-0622/seelex/contract/dto"',
        '"github.com/RedHuang-0622/seelex/contract/dto"\n\tprotocol "github.com/RedHuang-0622/seelex/contract/protocol"',
        1,
    )
    source = replace_between(
        source,
        "func (port SessionPort) SaveSessionSnapshot(",
        "// LoadEventRangeWorkspace",
        SNAPSHOT,
    )
    source = replace_between(
        source,
        "func (port SessionPort) SaveSessionSnapshotWorkspace(",
        "// LoadContextStateWorkspace",
        WORKSPACE,
    )
    return source.rstrip() + "\n" + ADAPTER


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--manifest", required=True)
    parser.add_argument("--checkpoint", required=True)
    parser.add_argument("--worktree", required=True)
    parser.add_argument("--branch", required=True)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--dry-run", action="store_true")
    mode.add_argument("--apply", action="store_true")
    mode.add_argument("--rollback")
    args = parser.parse_args()
    worktree = Path(args.worktree).resolve()
    root = Path(subprocess.check_output(["git", "rev-parse", "--show-toplevel"], cwd=worktree, text=True).strip()).resolve()
    branch = subprocess.check_output(["git", "branch", "--show-current"], cwd=worktree, text=True).strip()
    if root != worktree or branch != args.branch or not branch.startswith("codex/store-ddd-"):
        raise SystemExit(f"wrong worktree/branch: {root} {branch}")
    if subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=worktree, text=True).strip() != args.checkpoint:
        raise SystemExit("HEAD does not match checkpoint")
    manifest = json.loads((worktree / args.manifest).read_text(encoding="utf-8"))

    if args.rollback:
        journal = json.loads(Path(args.rollback).read_text(encoding="utf-8"))
        for entry in journal["files"]:
            path = worktree / entry["path"]
            if entry["existed"]:
                path.write_bytes(bytes.fromhex(entry["before_hex"]))
            elif path.exists():
                path.unlink()
        print("rolled back M4 external batch")
        return 0

    wiring = worktree / "wiring/adapters/session_workspace_ports.go"
    app = worktree / "application/conversation/turn_store_commit.go"
    expected = next(entry for entry in manifest["files"] if entry["action"] == "replace")
    if digest(wiring) != expected["sha256"]:
        raise SystemExit("wiring manifest hash mismatch")
    if app.exists():
        raise SystemExit("M4 app file already exists")
    before = wiring.read_bytes()
    after = build_adapter(before.decode("utf-8")).encode("utf-8")
    print(f"add {app.relative_to(worktree)}")
    print(f"replace {wiring.relative_to(worktree)} {len(before)} -> {len(after)} bytes")
    if args.dry_run:
        return 0
    journal = {"files": [
        {"path": str(app.relative_to(worktree)), "existed": False, "before_hex": ""},
        {"path": str(wiring.relative_to(worktree)), "existed": True, "before_hex": before.hex()},
    ]}
    journal_path = Path(tempfile.gettempdir()) / f"seelex-store-ddd-M4-{os.getpid()}.json"
    journal_path.write_text(json.dumps(journal), encoding="utf-8")
    try:
        app.parent.mkdir(parents=True, exist_ok=True)
        app.write_bytes(APP_FILE.encode("utf-8"))
        wiring.write_bytes(after)
        subprocess.run(["gofmt", "-w", str(app), str(wiring)], cwd=worktree, check=True)
    except Exception as exc:
        if app.exists():
            app.unlink()
        wiring.write_bytes(before)
        print(f"apply failed and rolled back: {exc}")
        return 1
    print(f"journal: {journal_path}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
