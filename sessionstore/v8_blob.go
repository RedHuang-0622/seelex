// v8 big_tool_result 旁路存储（主会话统一持有，M4）。
//
// 事实模型（my_design §6/§11）：
//   - 软限截断 + result_ref（默认 60000 字符）；硬限不落盘直接报错
//     （16 MB）；会话配额（64 MB）+ GC（T-BL-01/02/03）；
//   - subagent 不建独立 blob 目录，复用主会话 big_tool_result（I11）；GC
//     引用扫描需跨主会话与子代理消息（T-FK-09）。
package sessionstore

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	v8BlobSoftLimitChars = 60000
	v8BlobHardLimitBytes = 16 << 20
	v8BlobQuotaBytes     = 64 << 20
	v8BlobRefPrefix      = "blob:"
)

var (
	ErrV8BlobTooLarge = errors.New("v8: big tool result exceeds hard limit")
	ErrV8BlobQuota    = errors.New("v8: big tool result session quota exceeded")
)

// v8Blob 是 big_tool_result 索引/内容结构（JSONL 一行 = 一条）。
type v8Blob struct {
	Hash      string    `json:"hash"`
	SessionID string    `json:"session_id"`
	Kind      string    `json:"kind"`
	Size      int       `json:"size"`
	Truncated bool      `json:"truncated,omitempty"`
	Content   string    `json:"content,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func (store *v8Store) v8BlobDir(key Key) string {
	return filepath.Join(store.v8SessionRoot(key), "big_tool_result")
}

func (store *v8Store) v8BlobPath(key Key, hash string) string {
	return filepath.Join(store.v8BlobDir(key), hash+".jsonl")
}

// v8WriteBlob 写超大工具输出（主会话键）。返回 blob（Content 为截断后正文；
// 调用方把 Ref = blob:hash 写进消息行）。
func (store *v8Store) v8WriteBlob(key Key, toolName, content string) (v8Blob, error) {
	if len(content) > v8BlobHardLimitBytes {
		return v8Blob{}, ErrV8BlobTooLarge
	}
	hash := hash(content)
	truncated := len(content) > v8BlobSoftLimitChars
	if truncated {
		content = content[:v8BlobSoftLimitChars]
	}
	blob := v8Blob{
		Hash: hash, SessionID: key.SessionID, Kind: toolName,
		Size: len(content), Truncated: truncated, Content: content,
		CreatedAt: time.Now().UTC(),
	}
	dir := store.v8BlobDir(key)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return v8Blob{}, err
	}
	// 配额检查：dry-run 扫描现有 blob 总量 + 新增。
	total := int64(0)
	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, entry := range entries {
			if info, statErr := entry.Info(); statErr == nil {
				total += info.Size()
			}
		}
	}
	if total+int64(len(content)) > v8BlobQuotaBytes {
		return v8Blob{}, ErrV8BlobQuota
	}
	data, err := json.Marshal(blob)
	if err != nil {
		return v8Blob{}, err
	}
	if err := os.WriteFile(store.v8BlobPath(key, hash), append(data, '\n'), 0o600); err != nil {
		return v8Blob{}, err
	}
	return blob, nil
}

// v8ReadBlob 读取 blob 正文（返回截断后内容）。
func (store *v8Store) v8ReadBlob(key Key, hash string) (string, error) {
	data, err := os.ReadFile(store.v8BlobPath(key, hash))
	if errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		return "", errors.New("v8: empty blob")
	}
	var blob v8Blob
	if err := json.Unmarshal([]byte(lines[0]), &blob); err != nil {
		return "", err
	}
	return blob.Content, nil
}

// v8ListBlobHashes 枚举会话 blob（GC 引用扫描基础）。
func (store *v8Store) v8ListBlobHashes(key Key) ([]string, error) {
	entries, err := os.ReadDir(store.v8BlobDir(key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var hashes []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".jsonl") {
			hashes = append(hashes, strings.TrimSuffix(entry.Name(), ".jsonl"))
		}
	}
	sort.Strings(hashes)
	return hashes, nil
}

// v8BlobGarbageCollect 删除无引用 blob（referenced 跨主会话与子代理消息）。
// 返回删除列表。dryRun=true 只输出候选不删除（T-BL-03）。
func (store *v8Store) v8BlobGarbageCollect(key Key, referenced map[string]bool, dryRun bool) ([]string, error) {
	hashes, err := store.v8ListBlobHashes(key)
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, blobHash := range hashes {
		if referenced[blobHash] {
			continue
		}
		removed = append(removed, blobHash)
		if !dryRun {
			_ = os.Remove(store.v8BlobPath(key, blobHash))
		}
	}
	return removed, nil
}

// v8BlobRefOf 归一化 result_ref 为 blob hash。
func v8BlobRefOf(ref string) string {
	return strings.TrimPrefix(ref, v8BlobRefPrefix)
}
