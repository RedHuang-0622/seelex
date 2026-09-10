// big_tool_result 旁路存储（主会话统一持有，M4）。
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

const blobRefPrefix = "blob:"

// 三条限额（软截断 / 硬报错 / 会话配额）取自 §11 settings，不在此硬编码。
func (store *storeEngine) blobLimits() (softChars int, hardBytes int, quotaBytes int) {
	settings := store.settings
	return settings.BlobSoftLimitChars, settings.BlobHardLimitBytes, settings.BlobSessionQuotaBytes
}

var (
	ErrBigToolResultTooLarge = errors.New("session storage: big tool result exceeds hard limit")
	ErrBigToolResultQuota    = errors.New("session storage: big tool result session quota exceeded")
)

// blob 是 big_tool_result 索引/内容结构（JSONL 一行 = 一条）。
type toolBlob struct {
	Hash      string    `json:"hash"`
	SessionID string    `json:"session_id"`
	Kind      string    `json:"kind"`
	Size      int       `json:"size"`
	Truncated bool      `json:"truncated,omitempty"`
	Content   string    `json:"content,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func (store *storeEngine) blobDir(key Key) string {
	return filepath.Join(store.sessionRoot(key), "big_tool_result")
}

func (store *storeEngine) blobPath(key Key, hash string) string {
	return filepath.Join(store.blobDir(key), hash+".jsonl")
}

// writeBlob 写超大工具输出（主会话键）。返回 blob（Content 为截断后正文；
// 调用方把 Ref = blob:hash 写进消息行）。
func (store *storeEngine) writeBlob(key Key, toolName, content string) (toolBlob, error) {
	softChars, hardBytes, _ := store.blobLimits()
	if len(content) > hardBytes {
		return toolBlob{}, ErrBigToolResultTooLarge
	}
	hash := hash(content)
	truncated := len(content) > softChars
	if truncated {
		content = content[:softChars]
	}
	blob := toolBlob{
		Hash: hash, SessionID: key.SessionID, Kind: toolName,
		Size: len(content), Truncated: truncated, Content: content,
		CreatedAt: time.Now().UTC(),
	}
	dir := store.blobDir(key)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return toolBlob{}, err
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
	_, _, quotaBytes := store.blobLimits()
	if total+int64(len(content)) > int64(quotaBytes) {
		return toolBlob{}, ErrBigToolResultQuota
	}
	data, err := json.Marshal(blob)
	if err != nil {
		return toolBlob{}, err
	}
	if err := os.WriteFile(store.blobPath(key, hash), append(data, '\n'), 0o600); err != nil {
		return toolBlob{}, err
	}
	return blob, nil
}

// readBlob 读取 blob 正文（返回截断后内容）。
func (store *storeEngine) readBlob(key Key, hash string) (string, error) {
	data, err := os.ReadFile(store.blobPath(key, hash))
	if errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		return "", errors.New("session storage: empty blob")
	}
	var blob toolBlob
	if err := json.Unmarshal([]byte(lines[0]), &blob); err != nil {
		return "", err
	}
	return blob.Content, nil
}

// listBlobHashes 枚举会话 blob（GC 引用扫描基础）。
func (store *storeEngine) listBlobHashes(key Key) ([]string, error) {
	entries, err := os.ReadDir(store.blobDir(key))
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

// blobGarbageCollect 删除无引用 blob（referenced 跨主会话与子代理消息）。
// 返回删除列表。dryRun=true 只输出候选不删除（T-BL-03）。
func (store *storeEngine) blobGarbageCollect(key Key, referenced map[string]bool, dryRun bool) ([]string, error) {
	hashes, err := store.listBlobHashes(key)
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
			_ = os.Remove(store.blobPath(key, blobHash))
		}
	}
	return removed, nil
}

// blobRefOf 归一化 result_ref 为 blob hash。
func blobRefOf(ref string) string {
	return strings.TrimPrefix(ref, blobRefPrefix)
}
