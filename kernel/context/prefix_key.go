package context_runtime

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/context/prefixprobe"
	task_context "github.com/RedHuang-0622/seelex/kernel/task"
)

// PrefixWorld 是 prefix_key 中由装配层拥有、不能由 context 域另存一份的世界格。
type PrefixWorld struct {
	ProjectVersion string
	CompactSeq     uint64
	ToolsEpoch     uint64
}

// PrefixWorldPort 是 Runtime 的窄可选能力。未实现时世界格取零值，不阻塞装配。
type PrefixWorldPort interface {
	PrefixWorld() PrefixWorld
}

// MemoryEpochPort 由装配 Runtime 实现：首次装配/压缩滚动时选择一次 memory，
// 其余轮次只发布已持久化正文。
type MemoryEpochPort interface {
	FreezeMemoryEpoch(sessionID, query string) string
	SetMemoryEpoch(sessionID, content string)
}

type ToolCatalogEpochPort interface {
	SetToolCatalogForSession(sessionID string, epoch uint64, catalog []model.Tool)
}

const ToolsSentinelRule = "工具集声明规则：出现 ⟦toolset⟧ 块时，以最后一块为准，此前同名块作废。"

// PrefixKeyMaterials 是装配出口交给键函数的值语义素材。
type PrefixKeyMaterials struct {
	System       string
	Tools        []model.Tool
	Messages     []contract.EngineMessage
	RetainedFrom int
}

// BuildPrefixKey 由唯一键实现 prefixprobe.BuildKey 生成六格键。
func BuildPrefixKey(materials PrefixKeyMaterials, world PrefixWorld, compactSeq uint64) prefixprobe.Key {
	if compactSeq < world.CompactSeq {
		compactSeq = world.CompactSeq
	}
	signatures := make([]string, 0, len(materials.Tools))
	for _, tool := range materials.Tools {
		signatures = append(signatures, prefixprobe.ToolSignature(tool.Name, tool.Description))
	}
	return prefixprobe.BuildKey(prefixprobe.KeyInput{
		SystemPrompt:   materials.System,
		ProjectVersion: world.ProjectVersion,
		MemoryEpoch:    compactSeq,
		CompactSeq:     compactSeq,
		ToolsEpoch:     world.ToolsEpoch,
		ToolSignatures: signatures,
		RetainedFrom:   materials.RetainedFrom,
	})
}

func (c *Coordinator) prefixWorld(sessionID string) PrefixWorld {
	world := PrefixWorld{}
	if probe, ok := c.Deps.Runtime.(PrefixWorldPort); ok {
		world = probe.PrefixWorld()
	}
	if reader, ok := c.tasks.(interface {
		ContextEpochStateFor(string) model.SessionExecutionRecord
	}); ok {
		if epochs := reader.ContextEpochStateFor(sessionID); epochs.ToolsEpoch > 0 {
			world.ToolsEpoch = epochs.ToolsEpoch
		}
	}
	return world
}

func (c *Coordinator) frozenToolsFor(sessionID string, visible []model.Tool) []model.Tool {
	manager, ok := c.tasks.(interface {
		ContextEpochStateFor(string) model.SessionExecutionRecord
		ReplaceToolCatalogFor(string, []model.ToolCatalogEntry) (bool, uint64)
	})
	if !ok {
		c.publishToolCatalog(sessionID, 1, visible)
		return visible
	}
	catalog := manager.ContextEpochStateFor(sessionID).ToolCatalog
	if len(catalog) == 0 {
		entries := make([]model.ToolCatalogEntry, 0, len(visible))
		for _, tool := range visible {
			entries = append(entries, model.ToolCatalogEntry{Name: tool.Name, Description: tool.Description})
		}
		manager.ReplaceToolCatalogFor(sessionID, entries)
		catalog = manager.ContextEpochStateFor(sessionID).ToolCatalog
	}
	frozen := make([]model.Tool, 0, len(catalog))
	for _, entry := range catalog {
		frozen = append(frozen, model.Tool{Name: entry.Name, Description: entry.Description})
	}
	epoch := uint64(1)
	if state := manager.ContextEpochStateFor(sessionID); state.ToolsEpoch > 0 {
		epoch = state.ToolsEpoch
	}
	c.publishToolCatalog(sessionID, epoch, frozen)
	return frozen
}

func (c *Coordinator) publishToolCatalog(sessionID string, epoch uint64, catalog []model.Tool) {
	if port, ok := c.Deps.Runtime.(ToolCatalogEpochPort); ok {
		port.SetToolCatalogForSession(sessionID, epoch, catalog)
	}
}

func withToolsSentinelRule(systemPrompt string) string {
	if strings.Contains(systemPrompt, ToolsSentinelRule) {
		return systemPrompt
	}
	if strings.TrimSpace(systemPrompt) == "" {
		return ToolsSentinelRule
	}
	return systemPrompt + "\n\n" + ToolsSentinelRule
}

func (c *Coordinator) toolsSentinelFor(sessionID string, visible, frozen []model.Tool) string {
	manager, ok := c.tasks.(interface {
		ContextEpochStateFor(string) model.SessionExecutionRecord
		AppendContextToolsFor(string, uint64, string, []string, string, string) (model.ToolsUpdate, error)
	})
	if !ok {
		return ""
	}
	state := manager.ContextEpochStateFor(sessionID)
	if state.ToolsEpoch == 0 {
		return ""
	}
	names := toolNames(visible)
	var latest *model.ToolsUpdate
	for index := range state.ToolsUpdates {
		row := &state.ToolsUpdates[index]
		if row.Epoch <= state.ToolsEpoch {
			latest = row
		}
	}
	if latest == nil {
		sentinel := task_context.RenderToolsSentinel(state.ToolsEpoch, toolNames(frozen), nil, nil)
		_, _ = manager.AppendContextToolsFor(sessionID, state.ToolsEpoch, task_context.ToolsUpdateAdd,
			toolNames(frozen), sentinel, "catalog-freeze")
		return sentinel
	}
	added, removed := stringSetDiff(names, latest.ToolNames)
	if latest.Epoch < state.ToolsEpoch {
		sentinel := task_context.RenderToolsSentinel(state.ToolsEpoch, names, added, removed)
		_, _ = manager.AppendContextToolsFor(sessionID, state.ToolsEpoch, task_context.ToolsUpdateAdd,
			names, sentinel, "catalog-change")
		return sentinel
	}
	if len(added) == 0 && len(removed) == 0 {
		return ""
	}
	sentinel := task_context.RenderToolsSentinel(state.ToolsEpoch, names, added, removed)
	_, _ = manager.AppendContextToolsFor(sessionID, state.ToolsEpoch, task_context.ToolsUpdateRestate,
		names, sentinel, "availability-change")
	return sentinel
}

// EnsureToolsetSentinelFor runs at the turn boundary before the user transcript
// row. On epoch/availability change it appends exactly one internal system event;
// prepare then consumes that event by position instead of mutating current input.
func (c *Coordinator) EnsureToolsetSentinelFor(sessionID string) {
	if c == nil || c.Deps.Runtime == nil {
		return
	}
	visible := c.Deps.Runtime.VisibleTools(context.Background())
	frozen := c.frozenToolsFor(sessionID, visible)
	sentinel := c.toolsSentinelFor(sessionID, visible, frozen)
	if sentinel == "" {
		return
	}
	if appender, ok := c.tasks.(interface {
		AppendTranscriptEventForLocked(string, model.TranscriptEvent) model.TranscriptEvent
	}); ok {
		appender.AppendTranscriptEventForLocked(sessionID, model.TranscriptEvent{
			Role: "system", Kind: model.TranscriptEventKindInternal,
			Content: sentinel, WireMaterial: true,
		})
	}
}

func toolNames(tools []model.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}

func stringSetDiff(current, previous []string) (added, removed []string) {
	now := make(map[string]struct{}, len(current))
	before := make(map[string]struct{}, len(previous))
	for _, name := range current {
		now[name] = struct{}{}
	}
	for _, name := range previous {
		before[name] = struct{}{}
	}
	for name := range now {
		if _, ok := before[name]; !ok {
			added = append(added, name)
		}
	}
	for name := range before {
		if _, ok := now[name]; !ok {
			removed = append(removed, name)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

func (c *Coordinator) syncMemoryEpoch(sessionID, query string, epoch uint64) {
	port, ok := c.Deps.Runtime.(MemoryEpochPort)
	if !ok {
		return
	}
	manager, ok := c.tasks.(interface {
		ContextEpochStateFor(string) model.SessionExecutionRecord
		AppendContextMemoryFor(string, uint64, string, string, string, string) (model.MemoryUpdate, error)
	})
	if !ok {
		return
	}
	state := manager.ContextEpochStateFor(sessionID)
	hasEpoch := false
	for _, row := range state.MemoryUpdates {
		if row.Epoch == epoch && row.Kind == task_context.MemoryUpdateAdd {
			hasEpoch = true
			break
		}
	}
	if !hasEpoch {
		retired := make(map[string]bool)
		for _, row := range state.MemoryUpdates {
			if row.Kind == task_context.MemoryUpdateRetire {
				retired[row.SegmentID] = true
				continue
			}
			if row.Kind == task_context.MemoryUpdateAdd || row.Kind == task_context.MemoryUpdateSupersede {
				if !retired[row.SegmentID] {
					_, _ = manager.AppendContextMemoryFor(sessionID, epoch, task_context.MemoryUpdateRetire,
						row.SegmentID, "", "epoch-roll")
					retired[row.SegmentID] = true
				}
			}
		}
		content := port.FreezeMemoryEpoch(sessionID, query)
		_, _ = manager.AppendContextMemoryFor(sessionID, epoch, task_context.MemoryUpdateAdd,
			fmt.Sprintf("memory:%d", epoch), content, "epoch-freeze")
		state = manager.ContextEpochStateFor(sessionID)
	}
	content, err := task_context.FoldMemoryUpdates(state.MemoryUpdates, epoch)
	if err != nil {
		content = ""
	}
	port.SetMemoryEpoch(sessionID, content)
}

func (c *Coordinator) contextL1Store() interface {
	RecordContextL1(string, task_context.ContextL1Snapshot) task_context.ContextL1Stats
	ResumeContextL1(string, prefixprobe.Key, []model.TranscriptEvent, int, uint64) (task_context.ContextL1Snapshot, bool)
	ContextL1For(string) (task_context.ContextL1Snapshot, bool)
	ContextL1StatsFor(string) task_context.ContextL1Stats
	TokenGeneration() uint64
} {
	store, _ := c.tasks.(interface {
		RecordContextL1(string, task_context.ContextL1Snapshot) task_context.ContextL1Stats
		ResumeContextL1(string, prefixprobe.Key, []model.TranscriptEvent, int, uint64) (task_context.ContextL1Snapshot, bool)
		ContextL1For(string) (task_context.ContextL1Snapshot, bool)
		ContextL1StatsFor(string) task_context.ContextL1Stats
		TokenGeneration() uint64
	})
	return store
}

// PrefixKeyFor 返回最近一次装配出口使用的真 prefix_key。
func (c *Coordinator) PrefixKeyFor(sessionID string) (prefixprobe.Key, bool) {
	store := c.contextL1Store()
	if store == nil {
		return prefixprobe.Key{}, false
	}
	snapshot, ok := store.ContextL1For(sessionID)
	return snapshot.Key, ok
}

// PrefixL1StatsFor 返回最近一次 L1 命中统计。
func (c *Coordinator) PrefixL1StatsFor(sessionID string) (task_context.ContextL1Stats, bool) {
	store := c.contextL1Store()
	if store == nil {
		return task_context.ContextL1Stats{}, false
	}
	if _, ok := store.ContextL1For(sessionID); !ok {
		return task_context.ContextL1Stats{}, false
	}
	return store.ContextL1StatsFor(sessionID), true
}

func (c *Coordinator) resumeContextL1(
	sessionID string,
	key prefixprobe.Key,
	events []model.TranscriptEvent,
	retainedFrom int,
) (task_context.ContextL1Snapshot, bool) {
	store := c.contextL1Store()
	if store == nil {
		return task_context.ContextL1Snapshot{}, false
	}
	return store.ResumeContextL1(sessionID, key, events, retainedFrom, store.TokenGeneration())
}

func (c *Coordinator) recordContextL1(sessionID string, snapshot task_context.ContextL1Snapshot) task_context.ContextL1Stats {
	store := c.contextL1Store()
	if store == nil {
		return task_context.ContextL1Stats{}
	}
	return store.RecordContextL1(sessionID, snapshot)
}
