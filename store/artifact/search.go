package artifact

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	storelayout "github.com/RedHuang-0622/seelex/store/internal/layout"
	storeroot "github.com/RedHuang-0622/seelex/store/root"
)

// SearchHit is one keyword range, endpoints inclusive.
type SearchHit struct {
	FromSeq     uint64   `json:"from_seq"`
	ToSeq       uint64   `json:"to_seq"`
	HitCount    int      `json:"hit_count"`
	Snippets    []string `json:"snippets,omitempty"`
	SummaryOnly bool     `json:"summary_only,omitempty"`
}

// SearchRow is the artifact-owned view required to build a search index.
type SearchRow struct {
	Seq              uint64
	Content          string
	Name             string
	ReasoningContent string
	ToolCalls        []SearchToolCall
}

// SearchToolCall is the searchable subset of a tool call.
type SearchToolCall struct {
	Name      string
	Arguments string
}

// SearchIndex is metadata-index/search.json.
type SearchIndex struct {
	SessionID string                 `json:"session_id"`
	Tokens    map[string][]SearchHit `json:"tokens"`
	BuiltAt   string                 `json:"built_at,omitempty"`
	FromSeq   uint64                 `json:"from_seq"`
	ToSeq     uint64                 `json:"to_seq"`
}

// PutSearch builds and atomically publishes an index.
func (s *Store) PutSearch(scope storelayout.Scope, rows []SearchRow, toSeq, watermark uint64, compactSummary string) error {
	lock := s.lock(scope, SearchLockID)
	lock.Lock()
	defer lock.Unlock()
	index := SearchIndex{SessionID: scope.SessionID, Tokens: make(map[string][]SearchHit), FromSeq: 1, ToSeq: toSeq}
	for _, row := range rows {
		text := row.Content + "\n" + row.Name + "\n" + row.ReasoningContent
		for _, call := range row.ToolCalls {
			text += "\n" + call.Name + " " + call.Arguments
		}
		for _, token := range tokenize(text) {
			index.Tokens[token] = appendHit(index.Tokens[token], row.Seq, row.Seq, row.Content)
		}
	}
	if watermark > 0 && compactSummary != "" {
		for _, token := range tokenize(compactSummary) {
			index.Tokens[token] = appendSummaryHit(index.Tokens[token], 1, watermark)
		}
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	path := s.SearchPath(scope)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return storeroot.WriteAtomic(path, data, 0o600)
}

// Search reads and queries a session search index.
func (s *Store) Search(scope storelayout.Scope, query string) ([]SearchHit, error) {
	data, err := os.ReadFile(s.SearchPath(scope))
	if err != nil {
		return nil, err
	}
	var index SearchIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, err
	}
	if index.SessionID != scope.SessionID {
		return nil, errors.New("session storage: search index session mismatch")
	}
	seen := make(map[string]bool)
	var out []SearchHit
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

// IsSearchMissing reports the absent-index state.
func IsSearchMissing(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}

func appendHit(hits []SearchHit, fromSeq, toSeq uint64, snippet string) []SearchHit {
	if len(hits) > 0 && hits[len(hits)-1].ToSeq+1 == fromSeq && !hits[len(hits)-1].SummaryOnly {
		hits[len(hits)-1].ToSeq = toSeq
		hits[len(hits)-1].HitCount++
		return hits
	}
	text := strings.TrimSpace(snippet)
	if len(text) > 80 {
		text = text[:80]
	}
	return append(hits, SearchHit{FromSeq: fromSeq, ToSeq: toSeq, HitCount: 1, Snippets: []string{text}})
}

func appendSummaryHit(hits []SearchHit, fromSeq, toSeq uint64) []SearchHit {
	return append(hits, SearchHit{FromSeq: fromSeq, ToSeq: toSeq, HitCount: 1, SummaryOnly: true})
}

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
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || isCJK(r)
}

func isCJK(r rune) bool {
	return r >= 0x4E00 && r <= 0x9FFF
}

func sortSearchHits(hits []SearchHit) {
	for index := 1; index < len(hits); index++ {
		for j := index; j > 0 && hits[j].FromSeq < hits[j-1].FromSeq; j-- {
			hits[j], hits[j-1] = hits[j-1], hits[j]
		}
	}
}
