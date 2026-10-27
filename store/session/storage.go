package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
	storeroot "github.com/RedHuang-0622/seelex/store/root"
)

// DecodeMessageRows parses JSONL transcript rows, skipping empty rows and an
// unfinished crash tail.
func DecodeMessageRows(data []byte, onDecode func()) []protocol.TranscriptEvent {
	if onDecode != nil {
		onDecode()
	}
	segments := bytes.Split(data, []byte{'\n'})
	rows := make([]protocol.TranscriptEvent, 0, len(segments))
	for index, segment := range segments {
		if index == len(segments)-1 && len(bytes.TrimSpace(segment)) > 0 {
			continue
		}
		segment = bytes.TrimSpace(segment)
		if len(segment) == 0 {
			continue
		}
		var row protocol.TranscriptEvent
		if json.Unmarshal(segment, &row) != nil {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

// ReadMessageRowsFile reads all rows from an already-open shard.
func ReadMessageRowsFile(file *os.File, onDecode func()) ([]protocol.TranscriptEvent, error) {
	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if stat.Size() == 0 {
		return []protocol.TranscriptEvent{}, nil
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	return DecodeMessageRows(data, onDecode), nil
}

// ReadMessageRowsAt reads a shard from an optional committed-byte boundary.
func ReadMessageRowsAt(path string, offset int64, onDecode func()) ([]protocol.TranscriptEvent, error) {
	file, err := storeroot.OpenSharedRead(path)
	if errors.Is(err, fs.ErrNotExist) {
		return []protocol.TranscriptEvent{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if offset > 0 {
		stat, err := file.Stat()
		if err != nil {
			return nil, err
		}
		if offset >= stat.Size() {
			return []protocol.TranscriptEvent{}, nil
		}
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			return nil, err
		}
		data, err := io.ReadAll(file)
		if err != nil {
			return nil, err
		}
		return DecodeMessageRows(data, onDecode), nil
	}
	return ReadMessageRowsFile(file, onDecode)
}

// AppendMessageRows writes complete JSONL rows after repairing a crash tail.
func AppendMessageRows(path string, rows []protocol.TranscriptEvent) error {
	_, _, _, err := AppendMessageRowsEx(path, rows)
	return err
}

// AppendMessageRowsEx returns appended bytes, post-truncate pre-size and whether
// a crash tail was repaired.
func AppendMessageRowsEx(path string, rows []protocol.TranscriptEvent) (appended []byte, preSize int64, truncated bool, err error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, 0, false, err
	}
	before, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		file.Close()
		return nil, 0, false, err
	}
	if err := storeroot.TruncateCrashTail(file); err != nil {
		file.Close()
		return nil, 0, false, err
	}
	after, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		file.Close()
		return nil, 0, false, err
	}
	buffer := make([]byte, 0, 4096)
	for _, row := range rows {
		data, err := json.Marshal(row)
		if err != nil {
			file.Close()
			return nil, 0, false, fmt.Errorf("session storage: encode message row: %w", err)
		}
		buffer = append(buffer, data...)
		buffer = append(buffer, '\n')
	}
	if _, err := file.Write(buffer); err != nil {
		file.Close()
		return nil, 0, false, err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return nil, 0, false, err
	}
	if err := file.Close(); err != nil {
		return nil, 0, false, err
	}
	return buffer, after, after != before, nil
}

// DecodeStructuralEvents parses EVENT JSONL rows.
func DecodeStructuralEvents(data []byte) []StructuralEvent {
	segments := bytes.Split(data, []byte{'\n'})
	rows := make([]StructuralEvent, 0, len(segments))
	for index, segment := range segments {
		if index == len(segments)-1 && len(bytes.TrimSpace(segment)) > 0 {
			continue
		}
		segment = bytes.TrimSpace(segment)
		if len(segment) == 0 {
			continue
		}
		var row StructuralEvent
		if json.Unmarshal(segment, &row) != nil {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

// ReadStructuralEventsFile reads all EVENT rows from an open shard.
func ReadStructuralEventsFile(file *os.File) ([]StructuralEvent, error) {
	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if stat.Size() == 0 {
		return []StructuralEvent{}, nil
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	return DecodeStructuralEvents(data), nil
}

// ReadStructuralEventsAt reads an EVENT shard, treating absence as empty.
func ReadStructuralEventsAt(path string) ([]StructuralEvent, error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if errors.Is(err, fs.ErrNotExist) {
		return []StructuralEvent{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return ReadStructuralEventsFile(file)
}

// AppendStructuralEvents writes EVENT rows and returns the resulting byte size.
func AppendStructuralEvents(path string, rows []StructuralEvent) (int64, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return 0, err
	}
	if err := storeroot.TruncateCrashTail(file); err != nil {
		file.Close()
		return 0, err
	}
	if _, err := file.Seek(0, io.SeekEnd); err != nil {
		file.Close()
		return 0, err
	}
	buffer := make([]byte, 0, 4096)
	for _, row := range rows {
		data, err := json.Marshal(row)
		if err != nil {
			file.Close()
			return 0, err
		}
		buffer = append(buffer, data...)
		buffer = append(buffer, '\n')
	}
	if _, err := file.Write(buffer); err != nil {
		file.Close()
		return 0, err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return 0, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return 0, err
	}
	size := info.Size()
	if err := file.Close(); err != nil {
		return 0, err
	}
	return size, nil
}
