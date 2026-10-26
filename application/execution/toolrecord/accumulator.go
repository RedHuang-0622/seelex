package toolrecord

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// Accumulator is one session's in-memory tool-outcome ledger.
//
// The zero value is not usable; call NewAccumulator. A single mutex protects
// the accumulator because updates arrive only at tool completion boundaries.
type Accumulator struct {
	mu     sync.Mutex
	stats  map[string]*dto.ToolRecordStat
	order  []string
	total  int
	lastAt map[string]time.Time
	dirty  bool
}

// NewAccumulator constructs an empty in-memory tool ledger.
func NewAccumulator() *Accumulator {
	return &Accumulator{stats: map[string]*dto.ToolRecordStat{}, lastAt: map[string]time.Time{}}
}

// Record adds one tool call. Successful calls count too so the board has a
// denominator for failure rates.
func (accumulator *Accumulator) Record(tool string, ok bool, reason string) {
	if accumulator == nil {
		return
	}
	tool = strings.TrimSpace(tool)
	if tool == "" {
		return
	}
	at := time.Now().UTC()
	accumulator.mu.Lock()
	defer accumulator.mu.Unlock()
	stat, seen := accumulator.stats[tool]
	if !seen {
		stat = &dto.ToolRecordStat{Tool: tool}
		accumulator.stats[tool] = stat
		accumulator.order = append(accumulator.order, tool)
	}
	if ok {
		stat.Success++
	} else {
		stat.Failure++
		stat.Reasons = bumpReason(stat.Reasons, normalizeReason(reason))
		if at.After(accumulator.lastAt[tool]) {
			accumulator.lastAt[tool] = at
		}
	}
	stat.Total = stat.Success + stat.Failure
	stat.LastAt = accumulator.lastAt[tool]
	accumulator.total++
	accumulator.dirty = true
}

// Stats returns a deterministic snapshot ordered by total calls descending,
// then by first appearance.
func (accumulator *Accumulator) Stats() []dto.ToolRecordStat {
	if accumulator == nil {
		return nil
	}
	accumulator.mu.Lock()
	defer accumulator.mu.Unlock()
	stats := make([]dto.ToolRecordStat, 0, len(accumulator.order))
	for _, tool := range accumulator.order {
		stat, ok := accumulator.stats[tool]
		if !ok {
			continue
		}
		stats = append(stats, cloneStat(*stat))
	}
	sort.SliceStable(stats, func(i, j int) bool { return stats[i].Total > stats[j].Total })
	return stats
}

// Stat returns one tool's current totals.
func (accumulator *Accumulator) Stat(tool string) (dto.ToolRecordStat, bool) {
	if accumulator == nil {
		return dto.ToolRecordStat{}, false
	}
	accumulator.mu.Lock()
	defer accumulator.mu.Unlock()
	stat, ok := accumulator.stats[strings.TrimSpace(tool)]
	if !ok {
		return dto.ToolRecordStat{}, false
	}
	return cloneStat(*stat), true
}

// Totals returns total, success and failure counts.
func (accumulator *Accumulator) Totals() (int, int, int) {
	if accumulator == nil {
		return 0, 0, 0
	}
	accumulator.mu.Lock()
	defer accumulator.mu.Unlock()
	success, failure := 0, 0
	for _, stat := range accumulator.stats {
		success += stat.Success
		failure += stat.Failure
	}
	return accumulator.total, success, failure
}

// Snapshot returns a full persisted-shape snapshot and clears dirty.
func (accumulator *Accumulator) Snapshot() (dto.ToolRecordSnapshot, bool) {
	if accumulator == nil {
		return dto.ToolRecordSnapshot{}, false
	}
	accumulator.mu.Lock()
	stats := make([]dto.ToolRecordStat, 0, len(accumulator.order))
	for _, tool := range accumulator.order {
		stat, ok := accumulator.stats[tool]
		if !ok {
			continue
		}
		stats = append(stats, cloneStat(*stat))
	}
	dirty := accumulator.dirty
	accumulator.dirty = false
	accumulator.mu.Unlock()
	sort.SliceStable(stats, func(i, j int) bool { return stats[i].Total > stats[j].Total })
	return dto.ToolRecordSnapshot{At: time.Now().UTC(), Tools: stats}, dirty
}

// Load merges a persisted snapshot monotonically. It is used when a cold
// session is read back and must never reduce counts already observed.
func (accumulator *Accumulator) Load(snapshot dto.ToolRecordSnapshot) {
	if accumulator == nil {
		return
	}
	accumulator.mu.Lock()
	defer accumulator.mu.Unlock()
	for _, incoming := range snapshot.Tools {
		tool := strings.TrimSpace(incoming.Tool)
		if tool == "" {
			continue
		}
		stat, seen := accumulator.stats[tool]
		if !seen {
			stat = &dto.ToolRecordStat{Tool: tool}
			accumulator.stats[tool] = stat
			accumulator.order = append(accumulator.order, tool)
		}
		if incoming.Success > stat.Success {
			accumulator.total += incoming.Success - stat.Success
			stat.Success = incoming.Success
		}
		if incoming.Failure > stat.Failure {
			accumulator.total += incoming.Failure - stat.Failure
			stat.Failure = incoming.Failure
		}
		stat.Total = stat.Success + stat.Failure
		stat.Reasons = mergeReasons(stat.Reasons, incoming.Reasons)
		if incoming.LastAt.After(stat.LastAt) {
			stat.LastAt = incoming.LastAt
		}
		if incoming.LastAt.After(accumulator.lastAt[tool]) {
			accumulator.lastAt[tool] = incoming.LastAt
		}
	}
}

// Merge adds one leaf-stage delta. It deliberately differs from Load: Merge
// adds new calls, while Load reconciles a persisted subset monotonically.
func (accumulator *Accumulator) Merge(stats []dto.ToolRecordStat) {
	if accumulator == nil || len(stats) == 0 {
		return
	}
	accumulator.mu.Lock()
	defer accumulator.mu.Unlock()
	for _, incoming := range stats {
		tool := strings.TrimSpace(incoming.Tool)
		if tool == "" {
			continue
		}
		stat, seen := accumulator.stats[tool]
		if !seen {
			stat = &dto.ToolRecordStat{Tool: tool}
			accumulator.stats[tool] = stat
			accumulator.order = append(accumulator.order, tool)
		}
		stat.Success += incoming.Success
		stat.Failure += incoming.Failure
		for _, reason := range incoming.Reasons {
			if reason.Count == 0 {
				continue
			}
			stat.Reasons = addReason(stat.Reasons, normalizeReason(reason.Reason), reason.Count)
		}
		stat.Total = stat.Success + stat.Failure
		if incoming.LastAt.After(stat.LastAt) {
			stat.LastAt = incoming.LastAt
		}
		if incoming.LastAt.After(accumulator.lastAt[tool]) {
			accumulator.lastAt[tool] = incoming.LastAt
		}
		accumulator.total += incoming.Success + incoming.Failure
		accumulator.dirty = true
	}
}

func normalizeReason(reason string) string {
	trimmed := strings.ToLower(strings.TrimSpace(reason))
	for _, known := range dto.ToolReasonVocabulary() {
		if trimmed == known {
			return known
		}
	}
	return dto.ToolReasonOther
}

func bumpReason(reasons []dto.ToolRecordReason, reason string) []dto.ToolRecordReason {
	return addReason(reasons, reason, 1)
}

func addReason(reasons []dto.ToolRecordReason, reason string, count int) []dto.ToolRecordReason {
	if count <= 0 {
		return reasons
	}
	for index := range reasons {
		if reasons[index].Reason == reason {
			reasons[index].Count += count
			return reasons
		}
	}
	return append(reasons, dto.ToolRecordReason{Reason: reason, Count: count})
}

func mergeReasons(current, incoming []dto.ToolRecordReason) []dto.ToolRecordReason {
	for _, candidate := range incoming {
		reason := strings.TrimSpace(candidate.Reason)
		if reason == "" {
			continue
		}
		replaced := false
		for index := range current {
			if current[index].Reason != reason {
				continue
			}
			if candidate.Count > current[index].Count {
				current[index].Count = candidate.Count
			}
			replaced = true
			break
		}
		if !replaced {
			current = append(current, dto.ToolRecordReason{Reason: reason, Count: candidate.Count})
		}
	}
	return current
}

func cloneStat(stat dto.ToolRecordStat) dto.ToolRecordStat {
	if len(stat.Reasons) > 0 {
		stat.Reasons = append([]dto.ToolRecordReason(nil), stat.Reasons...)
	}
	return stat
}
