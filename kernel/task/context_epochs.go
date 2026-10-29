package task_context

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	model "github.com/RedHuang-0622/seelex/contract/protocol"
)

const (
	MemoryUpdateAdd       = "add"
	MemoryUpdateSupersede = "supersede"
	MemoryUpdateRetire    = "retire"

	ToolsUpdateAdd     = "add"
	ToolsUpdateRemove  = "remove"
	ToolsUpdateRestate = "restate"

	ToolsetSentinelOpen  = "⟦toolset⟧"
	ToolsetSentinelClose = "⟦/toolset⟧"
)

// ContextEpochStateFor 返回 memory/tools 纪元事实的深拷贝。
func (c *Coordinator) ContextEpochStateFor(sessionID string) model.SessionExecutionRecord {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	state := c.sessionStateLocked(sessionID).memoryLedger
	state.MemoryUpdates = append([]model.MemoryUpdate(nil), state.MemoryUpdates...)
	state.ToolsUpdates = append([]model.ToolsUpdate(nil), state.ToolsUpdates...)
	state.ToolCatalog = append([]model.ToolCatalogEntry(nil), state.ToolCatalog...)
	return state
}

// AppendMemoryUpdateFor appends one already-rendered epoch row.
func (c *Coordinator) AppendMemoryUpdateFor(sessionID string, update model.MemoryUpdate) error {
	if update.Seq == 0 || update.Digest == "" || !validMemoryKind(update.Kind) {
		return fmt.Errorf("memory update: invalid row")
	}
	if update.Kind != MemoryUpdateRetire && digestBytes(update.Bytes) != update.Digest {
		return fmt.Errorf("memory update: digest mismatch seq=%d", update.Seq)
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	state := &c.sessionStateLocked(sessionID).memoryLedger
	for _, existing := range state.MemoryUpdates {
		if existing.Seq == update.Seq {
			return nil
		}
	}
	if update.CreatedAt.IsZero() {
		update.CreatedAt = time.Now()
	}
	state.MemoryUpdates = append(state.MemoryUpdates, update)
	sort.SliceStable(state.MemoryUpdates, func(i, j int) bool {
		return state.MemoryUpdates[i].Seq < state.MemoryUpdates[j].Seq
	})
	return nil
}

// AppendContextMemoryFor assigns the next shared epoch seq and appends one
// already-rendered memory row.
func (c *Coordinator) AppendContextMemoryFor(
	sessionID string,
	epoch uint64,
	kind, segmentID, content, reason string,
) (model.MemoryUpdate, error) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	state := &c.sessionStateLocked(sessionID).memoryLedger
	seq := nextContextEpochSeq(state)
	update := model.MemoryUpdate{
		Seq: seq, Epoch: epoch, Kind: kind, SegmentID: segmentID,
		Digest: digestBytes(content), Bytes: content, Reason: reason,
		CreatedAt: time.Now(),
	}
	if !validMemoryKind(update.Kind) {
		return model.MemoryUpdate{}, fmt.Errorf("memory update: invalid kind")
	}
	state.MemoryUpdates = append(state.MemoryUpdates, update)
	sort.SliceStable(state.MemoryUpdates, func(i, j int) bool {
		return state.MemoryUpdates[i].Seq < state.MemoryUpdates[j].Seq
	})
	return update, nil
}

// FoldMemoryUpdates validates digests and returns the rendered bytes for epoch.
func FoldMemoryUpdates(updates []model.MemoryUpdate, epoch uint64) (string, error) {
	rows := append([]model.MemoryUpdate(nil), updates...)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Seq < rows[j].Seq })
	type segment struct {
		id    string
		bytes string
	}
	order := make([]string, 0)
	byID := make(map[string]string)
	for _, row := range rows {
		if row.Epoch > epoch {
			continue
		}
		if row.Kind != MemoryUpdateRetire && digestBytes(row.Bytes) != row.Digest {
			return "", fmt.Errorf("memory update digest mismatch seq=%d", row.Seq)
		}
		switch row.Kind {
		case MemoryUpdateAdd:
			if _, exists := byID[row.SegmentID]; !exists {
				order = append(order, row.SegmentID)
			}
			byID[row.SegmentID] = row.Bytes
		case MemoryUpdateSupersede:
			if _, exists := byID[row.SegmentID]; !exists {
				order = append(order, row.SegmentID)
			}
			byID[row.SegmentID] = row.Bytes
		case MemoryUpdateRetire:
			delete(byID, row.SegmentID)
		default:
			return "", fmt.Errorf("memory update unknown kind %q", row.Kind)
		}
	}
	parts := make([]string, 0, len(order))
	for _, id := range order {
		if value, ok := byID[id]; ok && value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, ""), nil
}

// ReplaceToolCatalogFor freezes the session catalog and bumps ToolsEpoch only
// when the catalog bytes actually change.
func (c *Coordinator) ReplaceToolCatalogFor(sessionID string, catalog []model.ToolCatalogEntry) (bool, uint64) {
	sorted := append([]model.ToolCatalogEntry(nil), catalog...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	state := &c.sessionStateLocked(sessionID).memoryLedger
	if toolCatalogDigest(state.ToolCatalog) == toolCatalogDigest(sorted) && state.ToolsEpoch > 0 {
		return false, state.ToolsEpoch
	}
	state.ToolCatalog = sorted
	state.ToolsEpoch++
	if state.ToolsEpoch == 0 {
		state.ToolsEpoch = 1
	}
	return true, state.ToolsEpoch
}

// AppendToolsUpdateFor appends one sentinel row after digest validation.
func (c *Coordinator) AppendToolsUpdateFor(sessionID string, update model.ToolsUpdate) error {
	if update.Seq == 0 || update.Epoch == 0 || update.Digest == "" {
		return fmt.Errorf("tools update: invalid row")
	}
	if digestBytes(update.SentinelBytes) != update.Digest {
		return fmt.Errorf("tools update: digest mismatch seq=%d", update.Seq)
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	state := &c.sessionStateLocked(sessionID).memoryLedger
	for _, existing := range state.ToolsUpdates {
		if existing.Seq == update.Seq {
			return nil
		}
	}
	if update.CreatedAt.IsZero() {
		update.CreatedAt = time.Now()
	}
	state.ToolsUpdates = append(state.ToolsUpdates, update)
	return nil
}

// AppendContextToolsFor assigns the next shared epoch seq and appends a sentinel row.
func (c *Coordinator) AppendContextToolsFor(
	sessionID string,
	epoch uint64,
	kind string,
	names []string,
	sentinel, reason string,
) (model.ToolsUpdate, error) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	state := &c.sessionStateLocked(sessionID).memoryLedger
	seq := nextContextEpochSeq(state)
	update := model.ToolsUpdate{
		Seq: seq, Epoch: epoch, Kind: kind,
		ToolNames: append([]string(nil), names...), Digest: digestBytes(sentinel),
		SentinelBytes: sentinel, Reason: reason, CreatedAt: time.Now(),
	}
	state.ToolsUpdates = append(state.ToolsUpdates, update)
	return update, nil
}

// FoldLatestToolSentinel returns the last valid sentinel at or below epoch.
func FoldLatestToolSentinel(updates []model.ToolsUpdate, epoch uint64) (string, error) {
	rows := append([]model.ToolsUpdate(nil), updates...)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Seq < rows[j].Seq })
	last := ""
	for _, row := range rows {
		if row.Epoch > epoch {
			continue
		}
		if digestBytes(row.SentinelBytes) != row.Digest {
			return "", fmt.Errorf("tools update digest mismatch seq=%d", row.Seq)
		}
		last = row.SentinelBytes
	}
	return last, nil
}

func toolCatalogDigest(catalog []model.ToolCatalogEntry) string {
	var builder strings.Builder
	for _, entry := range catalog {
		builder.WriteString(entry.Name)
		builder.WriteByte('\x00')
		builder.WriteString(entry.Description)
		builder.WriteByte('\x00')
	}
	return digestBytes(builder.String())
}

func digestBytes(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func validMemoryKind(kind string) bool {
	return kind == MemoryUpdateAdd || kind == MemoryUpdateSupersede || kind == MemoryUpdateRetire
}

func nextContextEpochSeq(state *model.SessionExecutionRecord) uint64 {
	var max uint64
	for _, row := range state.MemoryUpdates {
		if row.Seq > max {
			max = row.Seq
		}
	}
	for _, row := range state.ToolsUpdates {
		if row.Seq > max {
			max = row.Seq
		}
	}
	return max + 1
}

// RenderToolsSentinel renders the exact append-only tool availability block.
func RenderToolsSentinel(epoch uint64, available, added, removed []string) string {
	normalize := func(values []string) []string {
		out := append([]string(nil), values...)
		sort.Strings(out)
		for index := range out {
			out[index] = escapeToolsetDelimiters(out[index])
		}
		return out
	}
	renderList := func(values []string) string {
		if len(values) == 0 {
			return "(无)"
		}
		return strings.Join(values, ", ")
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "%s epoch=%d\n", ToolsetSentinelOpen, epoch)
	fmt.Fprintf(&builder, "可用: %s\n", renderList(normalize(available)))
	fmt.Fprintf(&builder, "本纪元新增: %s\n", renderList(normalize(added)))
	fmt.Fprintf(&builder, "本纪元移除: %s\n", renderList(normalize(removed)))
	builder.WriteString(ToolsetSentinelClose)
	return builder.String()
}

// ParseToolsSentinel accepts exactly one complete block and rejects truncation,
// duplicate open/close markers, or trailing declarations.
func ParseToolsSentinel(content string) (epoch uint64, err error) {
	content = strings.TrimSpace(content)
	if strings.Count(content, ToolsetSentinelOpen) != 1 || strings.Count(content, ToolsetSentinelClose) != 1 {
		return 0, fmt.Errorf("toolset sentinel: expected exactly one complete block")
	}
	if !strings.HasPrefix(content, ToolsetSentinelOpen) || !strings.HasSuffix(content, ToolsetSentinelClose) {
		return 0, fmt.Errorf("toolset sentinel: block is truncated or misordered")
	}
	firstLineEnd := strings.IndexByte(content, '\n')
	if firstLineEnd < 0 {
		return 0, fmt.Errorf("toolset sentinel: missing header")
	}
	if _, scanErr := fmt.Sscanf(content[len(ToolsetSentinelOpen):firstLineEnd], " epoch=%d", &epoch); scanErr != nil || epoch == 0 {
		return 0, fmt.Errorf("toolset sentinel: invalid epoch")
	}
	return epoch, nil
}

func escapeToolsetDelimiters(value string) string {
	value = strings.ReplaceAll(value, "⟦", `\u27e6`)
	return strings.ReplaceAll(value, "⟧", `\u27e7`)
}
