// Package board owns the append-only project-board projection, its bounded
// reads, compaction, per-path locks, and deterministic BOARD.md rendering.
//
// The board is derived data. It is not a fact source and does not participate
// in project, task, or teamwork decisions.
package board

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/contract/dto"
	storeroot "github.com/RedHuang-0622/seelex/store/root"
)

const (
	Schema          = dto.ProjectBoardSchema
	MaxRows         = dto.ProjectBoardMaxRows
	ReadMaxSessions = dto.ProjectBoardReadMaxSessions
	WorkFile        = dto.ProjectBoardWorkFile
	TeamworkFile    = dto.ProjectBoardTeamworkFile
	MarkdownFile    = dto.ProjectBoardMarkdownFile

	KindWork          = dto.ProjectBoardKindWork
	KindMilestone     = dto.ProjectBoardKindMilestone
	KindMilestoneItem = dto.ProjectBoardKindMilestoneItem
)

const (
	metaDirName  = ".seelex"
	boardDirName = "board"
	removedField = "removed"
)

var pathLocks sync.Map

// Kinds returns the accepted row-kind vocabulary in stable order.
func Kinds() []string {
	return []string{KindWork, KindMilestone, KindMilestoneItem}
}

func isKind(kind string) bool {
	switch kind {
	case KindWork, KindMilestone, KindMilestoneItem:
		return true
	default:
		return false
	}
}

// SessionFile validates a file in a session projection directory.
func SessionFile(file string) (string, error) {
	name := strings.TrimSpace(file)
	switch name {
	case WorkFile, TeamworkFile:
		return name, nil
	default:
		return "", fmt.Errorf("session storage: project board 文件名 %q 不在白名单（%s / %s）",
			file, WorkFile, TeamworkFile)
	}
}

// SessionSegment validates and normalizes a session ID used as a directory name.
func SessionSegment(sessionID string) (string, error) {
	segment := strings.TrimSpace(sessionID)
	if segment == "" {
		return "", errors.New("session storage: project board 需要会话 ID（投影按会话分目录）")
	}
	if segment == "." || segment == ".." {
		return "", fmt.Errorf("session storage: project board 会话 ID %q 不能当目录名", sessionID)
	}
	for _, r := range segment {
		switch {
		case r < 0x20 || r == 0x7f:
			return "", fmt.Errorf("session storage: project board 会话 ID %q 含控制字符", sessionID)
		case r == '/' || r == '\\' || r == ':' || r == '<' || r == '>' || r == '"' || r == '|' || r == '?' || r == '*':
			return "", fmt.Errorf("session storage: project board 会话 ID %q 含路径分隔符或非法字符", sessionID)
		}
	}
	if strings.HasSuffix(segment, ".") || strings.HasSuffix(segment, " ") {
		return "", fmt.Errorf("session storage: project board 会话 ID %q 不能以点或空格结尾", sessionID)
	}
	if len(segment) > 128 {
		return "", fmt.Errorf("session storage: project board 会话 ID 过长（%d 字节 > 128）", len(segment))
	}
	return segment, nil
}

// ValidateProjectRoot rejects an empty project root explicitly.
func ValidateProjectRoot(projectRoot string) error {
	if strings.TrimSpace(projectRoot) == "" {
		return errors.New("session storage: project root is required（未绑定工作区的会话没有看板落点）")
	}
	return nil
}

// Fold is the single read rule for append-only projection rows.
func Fold(rows []dto.ProjectBoardRow, logf func(string, ...any)) []dto.ProjectBoardRow {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	type foldKey struct{ session, key string }
	latest := make(map[foldKey]dto.ProjectBoardRow, len(rows))
	for _, row := range rows {
		if row.Schema != Schema {
			logf("project_board: 跳过未知 schema %d 的行（key=%q）", row.Schema, row.Key)
			continue
		}
		if !isKind(row.Kind) {
			logf("project_board: 跳过未知 kind %q 的行（key=%q）", row.Kind, row.Key)
			continue
		}
		key := strings.TrimSpace(row.Key)
		if key == "" {
			logf("project_board: 跳过没有 key 的行（kind=%q）", row.Kind)
			continue
		}
		bucket := foldKey{session: strings.TrimSpace(row.Scope.Session), key: key}
		if current, exists := latest[bucket]; !exists || row.Seq >= current.Seq {
			latest[bucket] = row
		}
	}
	folded := make([]dto.ProjectBoardRow, 0, len(latest))
	for _, row := range latest {
		if removed, ok := row.Payload[removedField].(bool); ok && removed {
			continue
		}
		folded = append(folded, row)
	}
	sort.SliceStable(folded, func(i, j int) bool {
		return rowLess(folded[i], folded[j])
	})
	return folded
}

func rowLess(left, right dto.ProjectBoardRow) bool {
	if left.Kind != right.Kind {
		return left.Kind < right.Kind
	}
	if left.Key != right.Key {
		return left.Key < right.Key
	}
	return left.Scope.Session < right.Scope.Session
}

// Dir returns the board directory beneath a project root.
func Dir(projectRoot string) string {
	return filepath.Join(projectRoot, metaDirName, boardDirName)
}

// SessionDir returns the conventional directory for one session.
func SessionDir(projectRoot, sessionID string) string {
	return filepath.Join(Dir(projectRoot), strings.TrimSpace(sessionID))
}

func sessionPath(projectRoot, sessionID, file string) (string, error) {
	if err := ValidateProjectRoot(projectRoot); err != nil {
		return "", err
	}
	segment, err := SessionSegment(sessionID)
	if err != nil {
		return "", err
	}
	name, err := SessionFile(file)
	if err != nil {
		return "", err
	}
	return filepath.Join(Dir(projectRoot), segment, name), nil
}

func markdownPath(projectRoot string) string {
	return filepath.Join(Dir(projectRoot), MarkdownFile)
}

// Mutex returns the single lock identity for a physical board path.
func Mutex(path string) *sync.Mutex {
	value, _ := pathLocks.LoadOrStore(path, &sync.Mutex{})
	return value.(*sync.Mutex)
}

func readFile(path string) ([]dto.ProjectBoardRow, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		var absent []dto.ProjectBoardRow
		return absent, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeRows(data), nil
}

func decodeRows(data []byte) []dto.ProjectBoardRow {
	segments := bytes.Split(data, []byte{'\n'})
	rows := make([]dto.ProjectBoardRow, 0, len(segments))
	for index, segment := range segments {
		if len(bytes.TrimSpace(segment)) == 0 || index == len(segments)-1 {
			continue
		}
		var row dto.ProjectBoardRow
		if err := json.Unmarshal(segment, &row); err != nil {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

func encodeRows(rows []dto.ProjectBoardRow) ([]byte, error) {
	buffer := make([]byte, 0, len(rows)*256)
	for _, row := range rows {
		line, err := json.Marshal(row)
		if err != nil {
			return nil, fmt.Errorf("session storage: 编码 project board 行: %w", err)
		}
		buffer = append(buffer, line...)
		buffer = append(buffer, '\n')
	}
	return buffer, nil
}

func batchAlreadyAppended(existing, rows []dto.ProjectBoardRow) bool {
	if len(existing) == 0 || len(rows) == 0 {
		return false
	}
	batchSeq := rows[0].Seq
	for _, row := range existing {
		if row.Seq == batchSeq {
			return true
		}
	}
	return false
}

func stampSession(sessionID string, rows []dto.ProjectBoardRow) error {
	for index := range rows {
		current := strings.TrimSpace(rows[index].Scope.Session)
		if current == "" {
			rows[index].Scope.Session = sessionID
			continue
		}
		if current != sessionID {
			return fmt.Errorf("session storage: project board 第 %d 行的会话 %q 与本目录的会话 %q 不一致（拒绝串写）",
				index, current, sessionID)
		}
	}
	return nil
}

func watermark(rows []dto.ProjectBoardRow) uint64 {
	var result uint64
	for _, row := range rows {
		if row.Seq > result {
			result = row.Seq
		}
	}
	return result
}

func maxSeq(rows []dto.ProjectBoardRow) uint64 {
	var result uint64
	for _, row := range rows {
		if row.Seq > result {
			result = row.Seq
		}
	}
	return result
}

func readRowsForAppend(path string) ([]dto.ProjectBoardRow, error) {
	handle, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	if err := storeroot.TruncateCrashTail(handle); err != nil {
		return nil, err
	}
	if _, err := handle.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(handle)
	if err != nil {
		return nil, err
	}
	return decodeRows(data), nil
}

func compactFile(path string, existing []dto.ProjectBoardRow) ([]dto.ProjectBoardRow, error) {
	compacted := Fold(existing, nil)
	data, err := encodeRows(compacted)
	if err != nil {
		return nil, err
	}
	if err := storeroot.WriteAtomic(path, data, 0o600); err != nil {
		return nil, fmt.Errorf("session storage: 压实 project board: %w", err)
	}
	return compacted, nil
}

// Append adds one batch and returns the file watermark.
func Append(projectRoot, sessionID, file string, rows []dto.ProjectBoardRow) (uint64, error) {
	path, err := sessionPath(projectRoot, sessionID, file)
	if err != nil {
		return 0, err
	}
	lock := Mutex(path)
	lock.Lock()
	defer lock.Unlock()

	if len(rows) == 0 {
		existing, readErr := readFile(path)
		if readErr != nil {
			return 0, readErr
		}
		return watermark(existing), nil
	}
	if len(rows) > MaxRows {
		return 0, fmt.Errorf("session storage: project board 本批行数 %d 越界（上限 %d）", len(rows), MaxRows)
	}
	segment, err := SessionSegment(sessionID)
	if err != nil {
		return 0, err
	}
	if err := stampSession(segment, rows); err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return 0, err
	}
	existing, err := readRowsForAppend(path)
	if err != nil {
		return 0, err
	}
	currentWatermark := watermark(existing)
	if batchAlreadyAppended(existing, rows) {
		return currentWatermark, nil
	}
	if len(existing)+len(rows) > MaxRows {
		compacted, compactErr := compactFile(path, existing)
		if compactErr != nil {
			return currentWatermark, compactErr
		}
		if len(compacted)+len(rows) > MaxRows {
			return currentWatermark, fmt.Errorf(
				"session storage: project board 行数越界（压实后活体 %d + 本批 %d > 上限 %d）",
				len(compacted), len(rows), MaxRows)
		}
		existing = compacted
	}
	if floor := watermark(existing); floor > currentWatermark {
		currentWatermark = floor
	}
	buffer, err := encodeRows(rows)
	if err != nil {
		return currentWatermark, err
	}
	handle, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return currentWatermark, err
	}
	if _, err := handle.Write(buffer); err != nil {
		handle.Close()
		return currentWatermark, err
	}
	if err := handle.Sync(); err != nil {
		handle.Close()
		return currentWatermark, err
	}
	if err := handle.Close(); err != nil {
		return currentWatermark, err
	}
	if next := maxSeq(rows); next > currentWatermark {
		currentWatermark = next
	}
	refreshMarkdown(projectRoot)
	return currentWatermark, nil
}

// ReadRows returns one session file folded to its current view.
func ReadRows(projectRoot, sessionID, file string) ([]dto.ProjectBoardRow, error) {
	path, err := sessionPath(projectRoot, sessionID, file)
	if err != nil {
		return nil, err
	}
	rows, err := readFile(path)
	if err != nil {
		return nil, err
	}
	return Fold(rows, nil), nil
}

// Watermark returns the maximum Seq in one session file, or zero when absent.
func Watermark(projectRoot, sessionID, file string) (uint64, error) {
	path, err := sessionPath(projectRoot, sessionID, file)
	if err != nil {
		return 0, err
	}
	rows, err := readFile(path)
	if err != nil {
		return 0, err
	}
	return watermark(rows), nil
}

// Read returns bounded, folded rows for one file across project sessions.
func Read(projectRoot string, opts dto.ProjectBoardReadOptions) ([]dto.ProjectBoardRow, bool, error) {
	file := strings.TrimSpace(opts.File)
	if file == "" {
		file = WorkFile
	}
	name, err := SessionFile(file)
	if err != nil {
		return nil, false, err
	}
	if err := ValidateProjectRoot(projectRoot); err != nil {
		return nil, false, err
	}
	exclude := strings.TrimSpace(opts.ExcludeSession)
	limitSessions := opts.MaxSessions
	if limitSessions <= 0 {
		limitSessions = ReadMaxSessions
	}
	folded := make([]dto.ProjectBoardRow, 0, 16)
	truncated := false
	sessions := 0
	for _, sessionDir := range sessionDirs(projectRoot) {
		if sessionDir == exclude {
			continue
		}
		if sessions >= limitSessions {
			truncated = true
			break
		}
		sessions++
		rows, readErr := readFile(filepath.Join(Dir(projectRoot), sessionDir, name))
		if readErr != nil {
			return nil, false, readErr
		}
		current := Fold(rows, nil)
		if opts.MaxRows > 0 && len(folded)+len(current) > opts.MaxRows {
			folded = append(folded, current[:opts.MaxRows-len(folded)]...)
			truncated = true
			break
		}
		folded = append(folded, current...)
	}
	sort.SliceStable(folded, func(i, j int) bool {
		return rowLess(folded[i], folded[j])
	})
	return folded, truncated, nil
}

func readFolded(projectRoot string) ([]dto.ProjectBoardRow, error) {
	var folded []dto.ProjectBoardRow
	for _, sessionDir := range sessionDirs(projectRoot) {
		for _, name := range []string{WorkFile, TeamworkFile} {
			raw, err := readFile(filepath.Join(Dir(projectRoot), sessionDir, name))
			if err != nil {
				return nil, err
			}
			folded = append(folded, Fold(raw, nil)...)
		}
	}
	sort.SliceStable(folded, func(i, j int) bool {
		return rowLess(folded[i], folded[j])
	})
	return folded, nil
}

func sessionDirs(projectRoot string) []string {
	entries, err := os.ReadDir(Dir(projectRoot))
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names
}

func refreshMarkdown(projectRoot string) {
	rows, err := readFolded(projectRoot)
	if err != nil {
		log.Printf("session storage: project board 人读件折叠读失败（%s）：%v", projectRoot, err)
		return
	}
	path := markdownPath(projectRoot)
	lock := Mutex(path)
	lock.Lock()
	defer lock.Unlock()
	if len(rows) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			log.Printf("session storage: project board 人读件删除失败（%s）：%v", path, err)
		}
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		log.Printf("session storage: project board 人读件目录建不出来（%s）：%v", path, err)
		return
	}
	if err := storeroot.WriteAtomic(path, Markdown(rows), 0o600); err != nil {
		log.Printf("session storage: project board 人读件重写失败（%s）：%v", path, err)
	}
}

// Reclaim removes one session's projection directory.
func Reclaim(projectRoot, sessionID string) error {
	if strings.TrimSpace(projectRoot) == "" || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	segment, err := SessionSegment(sessionID)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(Dir(projectRoot), segment)); err != nil {
		return err
	}
	refreshMarkdown(projectRoot)
	return nil
}

// ReadMarkdown reads the deterministic human-readable projection.
func ReadMarkdown(projectRoot string) ([]byte, error) {
	if err := ValidateProjectRoot(projectRoot); err != nil {
		return nil, err
	}
	return os.ReadFile(markdownPath(projectRoot))
}
