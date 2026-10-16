package residency

import (
	"errors"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// archiveFake 是归档用例的假宿主：记忆调用次序、落盘目标与释放面，并按用例需要
// 制造各轴拒绝与各步失败。它只实现端口，不模拟任何判据（判据就是被测对象）。
type archiveFake struct {
	sessionID string
	gate      ArchiveGate
	record    model.SessionRecord
	hasRecord bool
	loadErr   error
	flushErr  error
	unloadErr error
	markErr   error
	resident  bool
	loaded    bool

	gateCalls int
	calls     []string
	released  []string
	refreshed []string
}

func newArchiveFake() *archiveFake {
	return &archiveFake{
		sessionID: "session-a",
		record:    model.SessionRecord{ID: "session-a", Status: model.SessionStatusIdle},
		hasRecord: true,
	}
}

func (fake *archiveFake) deps() ArchiveDeps {
	return ArchiveDeps{
		Gate: func() ArchiveGate {
			fake.gateCalls++
			return fake.gate
		},
		Store: ArchiveStore{
			Load: func(sessionID string) (model.SessionRecord, bool, error) {
				fake.calls = append(fake.calls, "load")
				return fake.record, fake.hasRecord, fake.loadErr
			},
			Flush: func(sessionID string) error {
				fake.calls = append(fake.calls, "flush")
				return fake.flushErr
			},
			MarkArchived: func(sessionID string) error {
				fake.calls = append(fake.calls, "mark")
				return fake.markErr
			},
			RefreshProject: func(sessionID string) {
				fake.calls = append(fake.calls, "refresh")
				fake.refreshed = append(fake.refreshed, sessionID)
			},
		},
		Engine: ArchiveEngine{
			Resident: func(sessionID string) bool { return fake.resident },
			Loaded:   func(sessionID string) bool { return fake.loaded },
			Unload: func(sessionID string) error {
				fake.calls = append(fake.calls, "unload")
				return fake.unloadErr
			},
			ForgetResident: func(sessionID string) { fake.calls = append(fake.calls, "forget") },
		},
		ReleaseAsync: func(sessionID string) {
			fake.calls = append(fake.calls, "release")
			fake.released = append(fake.released, sessionID)
		},
	}
}

func (fake *archiveFake) archive() error {
	return Archive(fake.deps(), fake.sessionID)
}

func wantCalls(t *testing.T, fake *archiveFake, want ...string) {
	t.Helper()
	if len(fake.calls) != len(want) {
		t.Fatalf("调用次序 = %v, want %v", fake.calls, want)
	}
	for index, expected := range want {
		if fake.calls[index] != expected {
			t.Fatalf("调用次序 = %v, want %v", fake.calls, want)
		}
	}
}

// TestArchiveRejectsGateAxesInOrder 钉住门控的原次序：关闭 → 排空 → 忙 / 恢复中；
// 任一条拒绝都不碰存储与引擎（同迁移前"门控在一切动作之前"）。
func TestArchiveRejectsGateAxesInOrder(t *testing.T) {
	errClosed := errors.New("application is shut down")
	errDraining := errors.New("application is finishing active work")
	errBusy := errors.New("chat is already running")
	cases := []struct {
		name string
		gate ArchiveGate
		want error
	}{
		{"关闭优先于排空与忙态", ArchiveGate{
			Closed: true, Draining: true, Busy: true, Restoring: true,
			ErrClosed: errClosed, ErrDraining: errDraining, ErrBusy: errBusy,
		}, errClosed},
		{"排空优先于忙态", ArchiveGate{
			Draining: true, Busy: true, ErrDraining: errDraining, ErrBusy: errBusy,
		}, errDraining},
		{"忙态拒绝", ArchiveGate{Busy: true, ErrBusy: errBusy}, errBusy},
		{"恢复中复用忙态错误", ArchiveGate{Restoring: true, ErrBusy: errBusy}, errBusy},
		{"四轴皆空即放行", ArchiveGate{}, nil},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			fake := newArchiveFake()
			fake.gate = item.gate
			err := fake.archive()
			if item.want == nil {
				if err != nil {
					t.Fatalf("Archive = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, item.want) {
				t.Fatalf("Archive = %v, want %v", err, item.want)
			}
			if len(fake.calls) != 0 {
				t.Fatalf("门控拒绝仍触碰了宿主：%v", fake.calls)
			}
		})
	}
}

// TestArchiveGateRejectsWithoutAssembledError 钉住装配缺陷的口径：某轴拒绝却没带
// 错误值时报 ErrArchiveGateUnassembled，绝不静默放行（放行方向才是危险的）。
func TestArchiveGateRejectsWithoutAssembledError(t *testing.T) {
	cases := []struct {
		name string
		gate ArchiveGate
	}{
		{"关闭缺错误值", ArchiveGate{Closed: true}},
		{"排空缺错误值", ArchiveGate{Draining: true}},
		{"忙态缺错误值", ArchiveGate{Busy: true}},
		{"恢复中缺错误值", ArchiveGate{Restoring: true}},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			fake := newArchiveFake()
			fake.gate = item.gate
			if err := fake.archive(); !errors.Is(err, ErrArchiveGateUnassembled) {
				t.Fatalf("Archive = %v, want ErrArchiveGateUnassembled", err)
			}
			if len(fake.calls) != 0 {
				t.Fatalf("装配缺陷路径仍触碰了宿主：%v", fake.calls)
			}
		})
	}
}

// TestArchiveTrimsAndRejectsEmptySessionID 钉住空 ID 文案与"空 ID 不读宿主状态"：
// 门控端口在校验通过之前不该被调用。
func TestArchiveTrimsAndRejectsEmptySessionID(t *testing.T) {
	fake := newArchiveFake()
	err := Archive(fake.deps(), "   ")
	if err == nil || err.Error() != "session ID is required" {
		t.Fatalf("Archive(空 ID) = %v, want \"session ID is required\"", err)
	}
	if fake.gateCalls != 0 {
		t.Fatalf("空 ID 也读了门控读数：gateCalls=%d", fake.gateCalls)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("空 ID 触碰了宿主：%v", fake.calls)
	}
}

// TestArchiveSessionIDIsTrimmed 钉住 trim 口径：带空白的 ID 用去掉空白后的身份
// 读 record、写归档、释放与刷新（迁移前门面就是先 trim 再落点）。
func TestArchiveSessionIDIsTrimmed(t *testing.T) {
	fake := newArchiveFake()
	if err := Archive(fake.deps(), "  session-a\t"); err != nil {
		t.Fatalf("Archive = %v, want nil", err)
	}
	wantCalls(t, fake, "load", "mark", "release", "refresh")
	if len(fake.released) != 1 || fake.released[0] != "session-a" {
		t.Fatalf("释放的后台命令目标 = %v, want [session-a]", fake.released)
	}
	if len(fake.refreshed) != 1 || fake.refreshed[0] != "session-a" {
		t.Fatalf("刷新目录的会话 = %v, want [session-a]", fake.refreshed)
	}
}

// TestArchiveLoadFailureIsWrapped 钉住读面失败的文案与"失败即停"。
func TestArchiveLoadFailureIsWrapped(t *testing.T) {
	fake := newArchiveFake()
	fake.loadErr = errors.New("disk is on fire")
	err := fake.archive()
	if !errors.Is(err, fake.loadErr) {
		t.Fatalf("Archive = %v, want 包装 loadErr", err)
	}
	if !strings.HasPrefix(err.Error(), `archive load "session-a": `) {
		t.Fatalf("Archive = %q, want 前缀 `archive load \"session-a\": `", err.Error())
	}
	wantCalls(t, fake, "load")
}

// TestArchiveMissingOrMismatchedRecordIsNotFound 钉住"record 缺失"与"record ID 不符"
// 两条都归 errArchiveNotFound，且都带目标会话 ID。
func TestArchiveMissingOrMismatchedRecordIsNotFound(t *testing.T) {
	cases := []struct {
		name      string
		hasRecord bool
		recordID  string
	}{
		{"record 缺失", false, "session-a"},
		{"record 归属别的会话", true, "session-b"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			fake := newArchiveFake()
			fake.hasRecord = item.hasRecord
			fake.record = model.SessionRecord{ID: item.recordID, Status: model.SessionStatusIdle}
			err := fake.archive()
			if !errors.Is(err, ErrArchiveNotFound) {
				t.Fatalf("Archive = %v, want ErrArchiveNotFound", err)
			}
			if !strings.Contains(err.Error(), `"session-a"`) {
				t.Fatalf("Archive = %q, want 带目标会话 ID", err.Error())
			}
			wantCalls(t, fake, "load")
		})
	}
}

// TestArchiveAlreadyArchivedIsIdempotent 钉住幂等：record 已是 archived 时返回 nil，
// 且不碰引擎、不写盘、不释放后台命令、不刷新目录。
func TestArchiveAlreadyArchivedIsIdempotent(t *testing.T) {
	fake := newArchiveFake()
	fake.record = model.SessionRecord{ID: "session-a", Status: model.SessionStatusArchived}
	fake.resident = true
	fake.loaded = true
	if err := fake.archive(); err != nil {
		t.Fatalf("Archive = %v, want nil", err)
	}
	wantCalls(t, fake, "load")
}

// TestArchiveResidentOrderIsFlushUnloadForgetThenMark 钉住驻留会话的次序：
// flush → unload → 摘驻留序 → 写归档状态 → 释放后台命令 → 按项目刷新目录。
func TestArchiveResidentOrderIsFlushUnloadForgetThenMark(t *testing.T) {
	fake := newArchiveFake()
	fake.resident = true
	fake.loaded = true
	if err := fake.archive(); err != nil {
		t.Fatalf("Archive = %v, want nil", err)
	}
	wantCalls(t, fake, "load", "flush", "unload", "forget", "mark", "release", "refresh")
}

// TestArchiveSkipsEngineUnlessResidentAndLoaded 钉住引擎面的两个前置：单元没有驻留
// 标记、或引擎侧没有该会话实例时，都跳过 flush/unload（迁移前是"断言不上就不动引擎"）。
func TestArchiveSkipsEngineUnlessResidentAndLoaded(t *testing.T) {
	cases := []struct {
		name     string
		resident bool
		loaded   bool
	}{
		{"单元未驻留", false, true},
		{"引擎无该会话实例", true, false},
		{"两者皆无", false, false},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			fake := newArchiveFake()
			fake.resident = item.resident
			fake.loaded = item.loaded
			if err := fake.archive(); err != nil {
				t.Fatalf("Archive = %v, want nil", err)
			}
			wantCalls(t, fake, "load", "mark", "release", "refresh")
		})
	}
}

// TestArchiveFailuresStopTheChain 钉住三条写/释放失败的文案与"失败即停"：
// flush 失败不 unload，unload 失败不写归档状态，写归档失败不释放后台命令也不刷新目录。
func TestArchiveFailuresStopTheChain(t *testing.T) {
	cases := []struct {
		name     string
		prepare  func(fake *archiveFake)
		wantErr  error
		prefix   string
		wantCall []string
	}{
		{
			name: "flush 失败",
			prepare: func(fake *archiveFake) {
				fake.resident, fake.loaded = true, true
				fake.flushErr = errors.New("flush boom")
			},
			wantErr:  errors.New("flush boom"),
			prefix:   `archive flush "session-a": `,
			wantCall: []string{"load", "flush"},
		},
		{
			name: "unload 失败",
			prepare: func(fake *archiveFake) {
				fake.resident, fake.loaded = true, true
				fake.unloadErr = errors.New("unload boom")
			},
			wantErr:  errors.New("unload boom"),
			prefix:   `archive unload "session-a": `,
			wantCall: []string{"load", "flush", "unload"},
		},
		{
			name:     "写归档状态失败",
			prepare:  func(fake *archiveFake) { fake.markErr = errors.New("mark boom") },
			wantErr:  errors.New("mark boom"),
			prefix:   `archive "session-a": `,
			wantCall: []string{"load", "mark"},
		},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			fake := newArchiveFake()
			item.prepare(fake)
			err := fake.archive()
			if err == nil || !strings.Contains(err.Error(), item.wantErr.Error()) {
				t.Fatalf("Archive = %v, want 含 %v", err, item.wantErr)
			}
			if !strings.HasPrefix(err.Error(), item.prefix) {
				t.Fatalf("Archive = %q, want 前缀 %q", err.Error(), item.prefix)
			}
			wantCalls(t, fake, item.wantCall...)
			if len(fake.released) != 0 || len(fake.refreshed) != 0 {
				t.Fatalf("失败路径仍释放/刷新：released=%v refreshed=%v", fake.released, fake.refreshed)
			}
		})
	}
}
