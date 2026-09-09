// 单会话关键词模糊索引（M4）。
//
// 事实模型（my_design §4 I12）：索引 = 单会话关键词模糊索引，可重建、
// 允许落后；存 session/metadata-index/search.json；检索范围 = 本会话
// （不跨会话）；LRU 删除后重建：被删区只回摘要命中，无悬空原文。
package sessionstore

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// searchHit 是一个关键词命中区间（[FromSeq, ToSeq] 含端点）。
type searchHit struct {
	FromSeq     uint64   `json:"from_seq"`
	ToSeq       uint64   `json:"to_seq"`
	HitCount    int      `json:"hit_count"`
	Snippets    []string `json:"snippets,omitempty"`
	SummaryOnly bool     `json:"summary_only,omitempty"`
}

// searchIndex 是 metadata-index/search.json 内容（token → hits）。
type searchIndex struct {
	SessionID string                 `json:"session_id"`
	Tokens    map[string][]searchHit `json:"tokens"`
	BuiltAt   string                 `json:"built_at,omitempty"`
	FromSeq   uint64                 `json:"from_seq"`
	ToSeq     uint64                 `json:"to_seq"`
}

func (store *storeEngine) searchIndexPath(key Key) string {
	return filepath.Join(store.sessionRoot(key), "metadata-index", "search.json")
}

// rebuildSearchIndex 从 message 全量重建索引（可重建断言）；LRU 已删前缀
// 以 compact 摘要 token 承接（SummaryOnly）。
func (store *storeEngine) rebuildSearchIndex(key Key) error {
	store.mu(key, moduleMessage).Lock()
	head, err := store.readMessageHeadLocked(key)
	store.mu(key, moduleMessage).Unlock()
	if err != nil {
		return err
	}
	index := searchIndex{
		SessionID: key.SessionID,
		Tokens:    make(map[string][]searchHit),
		FromSeq:   1, ToSeq: head.LastSeq,
	}
	rows, err := store.readRows(key, 1, 0)
	if err != nil {
		return err
	}
	for _, row := range rows {
		text := row.Content + "\n" + row.Name + "\n" + row.ReasoningContent
		for _, call := range row.ToolCalls {
			text += "\n" + call.Name + " " + call.Arguments
		}
		for _, token := range tokenize(text) {
			index.Tokens[token] = appendHit(index.Tokens[token], row.Seq, row.Seq, row.Content)
		}
	}
	// 已淘汰前缀：用最新 compact 摘要承接（无悬空原文）。
	if head.WatermarkSeq > 0 {
		compact, err := store.readCompactHead(key)
		if err == nil && compact.LatestFrame != nil && compact.LatestFrame.Summary != "" {
			for _, token := range tokenize(compact.LatestFrame.Summary) {
				index.Tokens[token] = appendSummaryHit(index.Tokens[token], 1, head.WatermarkSeq)
			}
		}
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	path := store.searchIndexPath(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeAtomic(path, data, 0o600)
}

func appendHit(hits []searchHit, fromSeq, toSeq uint64, snippet string) []searchHit {
	if len(hits) > 0 && hits[len(hits)-1].ToSeq+1 == fromSeq && !hits[len(hits)-1].SummaryOnly {
		hits[len(hits)-1].ToSeq = toSeq
		hits[len(hits)-1].HitCount++
		return hits
	}
	text := strings.TrimSpace(snippet)
	if len(text) > 80 {
		text = text[:80]
	}
	return append(hits, searchHit{FromSeq: fromSeq, ToSeq: toSeq, HitCount: 1, Snippets: []string{text}})
}

func appendSummaryHit(hits []searchHit, fromSeq, toSeq uint64) []searchHit {
	return append(hits, searchHit{FromSeq: fromSeq, ToSeq: toSeq, HitCount: 1, SummaryOnly: true})
}

// tokenize 把中文/英文内容切成关键词 token（长度 ≥ 2；中文按单字滑窗
// 组合成双字词，模糊命中）。
func tokenize(text string) []string {
	seen := make(map[string]bool)
	var tokens []string
	push := func(token string) {
		token = strings.ToLower(strings.TrimSpace(token))
		if len(token) < 2 || seen[token] {
			return
		}
		seen[token] = true
		tokens = append(tokens, token)
	}
	var runeBuffer []rune
	flush := func() {
		if len(runeBuffer) == 0 {
			return
		}
		word := string(runeBuffer)
		push(word)
		// 中文字符滑窗双字词（关键词模糊）。
		runes := []rune(word)
		for index := 0; index+1 < len(runes); index++ {
			if isCJK(runes[index]) && isCJK(runes[index+1]) {
				push(string(runes[index : index+2]))
			}
		}
		runeBuffer = nil
	}
	for _, r := range text {
		if isTokenRune(r) {
			runeBuffer = append(runeBuffer, r)
		} else {
			flush()
		}
	}
	flush()
	return tokens
}

func isTokenRune(r rune) bool {
	if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || isCJK(r) {
		return true
	}
	return false
}

func isCJK(r rune) bool {
	return r >= 0x4E00 && r <= 0x9FFF
}

// searchQuery 模糊检索本会话（允许落后：索引为上次重建快照；缺失时先
// 重建一次）。query 也走同一 tokenize 滑窗。
func (store *storeEngine) searchQuery(key Key, query string) ([]searchHit, error) {
	path := store.searchIndexPath(key)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := store.rebuildSearchIndex(key); err != nil {
			return nil, err
		}
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}
	var index searchIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, err
	}
	if index.SessionID != key.SessionID {
		return nil, errors.New("session storage: search index session mismatch")
	}
	// 关键词模糊：query token 与索引 token 互相包含即命中。
	seen := make(map[string]bool)
	var out []searchHit
	for _, queryToken := range tokenize(query) {
		for token, hits := range index.Tokens {
			if seen[token] || !(strings.Contains(token, queryToken) || strings.Contains(queryToken, token)) {
				continue
			}
			seen[token] = true
			out = append(out, hits...)
		}
	}
	sortSearchHits(out)
	return out, nil
}

func sortSearchHits(hits []searchHit) {
	// 稳定排序：按 FromSeq 升序。
	for index := 1; index < len(hits); index++ {
		for j := index; j > 0 && hits[j].FromSeq < hits[j-1].FromSeq; j-- {
			hits[j], hits[j-1] = hits[j-1], hits[j]
		}
	}
}
