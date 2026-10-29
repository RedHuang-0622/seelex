package task_context

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// PlanContextPrefix 是 plan 上下文帧（计划投影 + 执行政策段）的固定前导标记。
//
// 它同时是**分类键**：装配侧据此把这一格识别为 plan 帧（而不是普通对话消息），
// 探针据此把失效归因到 plan 段（见 docs/arch/context-prefix-stability-l1.md §6.3.2、
// §7.4），事件侧据此认出「这是一条 plan 帧记录」。三处读者共用同一个字面量，
// 口径才不会分叉。
const PlanContextPrefix = "<!-- seelex:active-plan:v1 -->"

// PlanContextMessage 渲染 plan 上下文帧正文（计划投影 JSON + 执行政策段）。
//
// 这是 plan 帧正文的**唯一**渲染点：装配侧（kernel/context）与记录侧（本包的
// 录帧路径）都调它，两份正文因此逐字节同源——正文一漂移，"同一 plan_hash 一条
// 不发"的去重判据就会失效。
//
// plan 帧为什么必须落 transcript（而不是每轮现插在历史尾部）：见 §6.3.2 的实测。
// 每轮现插的位置随历史变长而后移，上一轮 plan 占的那一格被新历史占住，从该格
// 起整段重算；只有在**记录位置**重放才可能让前缀成立。
func PlanContextMessage(projection *model.ActivePlanProjection, frame *model.SessionPlanFrame) string {
	if projection == nil {
		return ""
	}
	payload := map[string]any{
		"plan_ref": projection.CanonicalPlanRef, "plan_id": projection.PlanID,
		"status": projection.Status, "current": projection.CurrentNode,
		"completed": projection.CompletedNodes, "failed": projection.FailedNodes,
		"pending": projection.PendingNodes,
	}
	if frame != nil {
		payload["current_slice"] = currentPlanSlice(frame.Arguments, projection.CurrentNode)
	}
	encoded, _ := json.Marshal(payload)
	// plan 执行指令并入帧正文（原本在 system 的 Active Plan Execution Policy 段，
	// 会随 plan 加载/完成改写 system 头部 → 一次前缀悬崖）。
	return PlanContextPrefix + "\n" + string(encoded) + "\n\n## Active Plan Execution Policy\n" +
		"The Plan is validated and authoritative for this task. Do not silently replace or reorder it. " +
		"Execute the current node and its declared dependencies in stable order. Use read_plan for omitted node detail. " +
		"plan_ref=" + projection.CanonicalPlanRef
}

// PlanContextClearedMessage 渲染 plan 帧的**墓碑**：计划已收尾/清除时追加一条
// 状态帧，让「后写覆盖前写」的读法约定覆盖掉历史里那条旧计划。
//
// 为什么必须有墓碑：帧一旦落进 transcript 就永久留史（D1 ③：旧帧不删）。计划
// 完成或清除时若一条不发，模型会继续把墙上那张旧计划当成权威——比"多发一条
// 状态帧"危险得多。
func PlanContextClearedMessage(planID, status string) string {
	payload := map[string]any{"plan_id": planID, "status": status}
	if planID != "" {
		payload["plan_ref"] = planID
	}
	encoded, _ := json.Marshal(payload)
	return PlanContextPrefix + "\n" + string(encoded) + "\n\n## Active Plan Execution Policy\n" +
		"No plan is active for this task. Do not resume or invent plan nodes; " +
		"follow the current request or load a new plan explicitly."
}

func currentPlanSlice(arguments, currentNode string) any {
	var plan struct {
		Nodes map[string]json.RawMessage `json:"nodes"`
		Edges map[string][]string        `json:"edges"`
	}
	if json.Unmarshal([]byte(arguments), &plan) != nil {
		return nil
	}
	nodes := make(map[string]json.RawMessage)
	edges := make(map[string][]string)
	if node, ok := plan.Nodes[currentNode]; ok {
		nodes[currentNode] = node
	}
	for source, targets := range plan.Edges {
		if source == currentNode {
			edges[source] = append([]string(nil), targets...)
			for _, target := range targets {
				if node, ok := plan.Nodes[target]; ok {
					nodes[target] = node
				}
			}
		}
		for _, target := range targets {
			if target == currentNode {
				edges[source] = AppendUniqueStrings(edges[source], target)
				if node, ok := plan.Nodes[source]; ok {
					nodes[source] = node
				}
			}
		}
	}
	return map[string]any{"nodes": nodes, "edges": edges}
}

// planFrameHash 是 plan 帧的**内容哈希**（sha256 hex）：同一份帧正文 ⇒ 同一个
// 哈希 ⇒ 一条不发（D1 ②）。用它而不是 plan 的逻辑版本号，是因为真正要判的是
// "已发出的那串字节会不会变"——版本号变了但正文没变时，多发一条只会把请求
// 前缀往后推一格。
func planFrameHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// isPlanFrameEvent 判定一条 transcript 事件是不是 plan 帧记录。
func isPlanFrameEvent(event model.TranscriptEvent) bool {
	if event.Role != "user" {
		return false
	}
	content := event.Content
	if event.ProviderContent != "" {
		content = event.ProviderContent
	}
	return strings.HasPrefix(content, PlanContextPrefix)
}

// planFrameBodyLocked 渲染当前会话的 plan 帧正文（无激活计划且计划未收尾 → 空）。
func (c *Coordinator) planFrameBodyLocked(st *sessionTaskRuntime) string {
	if c == nil || c.Core == nil {
		// 未装配快照的宿主（部分单测）没有 plan 事实可渲染：一条不发。
		return ""
	}
	projection := ActivePlanProjection(c.Snapshot.Runtime.Plan, st.activePlanID, st.planSequence)
	if projection == nil {
		return ""
	}
	if projection.Status == model.PlanCompleted.String() {
		return PlanContextClearedMessage(projection.PlanID, model.PlanCompleted.String())
	}
	return PlanContextMessage(projection, ActivePlanFrame(st.planStack, st.activePlanID))
}

// seedPlanFrameHashLocked 从 transcript 里最后一条 plan 帧记录恢复去重基线。
//
// 冷加载/恢复后内存里的去重基线是空的，不先对齐就会在**每一次装载**时重发一条
// 同内容帧（记录越长重复越多）。定位不到（这条会话还没有 plan 帧，或保留窗口
// 已经滚过它）就留空：宁可多发一条，也不能把一份没发出过的计划当成已发。
func (c *Coordinator) seedPlanFrameHashLocked(st *sessionTaskRuntime) {
	for index := len(st.transcript) - 1; index >= 0; index-- {
		event := st.transcript[index]
		if !isPlanFrameEvent(event) {
			continue
		}
		content := event.Content
		if event.ProviderContent != "" {
			content = event.ProviderContent
		}
		st.planFrameHash = planFrameHash(content)
		return
	}
	st.planFrameHash = ""
}

// recordPlanFrameLocked 把「当前 plan 帧」按记录位置落进 transcript：内容没变
// ⇒ 一条不发；变了 ⇒ 尾部追加一条（旧帧留史，靠后写覆盖前写读）。
//
// 调用时机是**计划状态写入的那一刻**（装载/恢复/节点打点/清除），而不是装配时
// 现插：帧必须落在「本回合用户提问事件之前」才可能前缀成立——装配那一刻用户事件
// 已经在 transcript 尾部了，此时追加会让帧排到它后面，下一轮重放时帧与提问的
// 先后就对调了（§6.3.2 的断点正是这一格）。
//
// 返回值 = 这次是否真的追加了一条事件。
func (c *Coordinator) recordPlanFrameLocked(st *sessionTaskRuntime) bool {
	if st == nil {
		return false
	}
	if st.planFrameHash == "" && len(st.transcript) > 0 {
		c.seedPlanFrameHashLocked(st)
	}
	body := c.planFrameBodyLocked(st)
	if body == "" {
		// 还没有记录过 plan 帧：没有可覆盖的旧计划，一条不发。
		if st.planFrameHash == "" {
			return false
		}
		// 记录过而计划没了：发墓碑覆盖旧帧（见 PlanContextClearedMessage）。
		body = PlanContextClearedMessage(st.activePlanID, "none")
	}
	digest := planFrameHash(body)
	if digest == st.planFrameHash {
		return false
	}
	c.appendTranscriptEventLocked(st, model.TranscriptEvent{
		Role: "user", Kind: model.TranscriptEventKindInternal, Content: body,
		// S19/D8：internal 行必须带 wire_material 才会在重启后进装配。
		// `_AppendTranscriptEventLocked` 对内部材料的置位同源（这里走的是已持
		// stateMu 的私有追加路径，不会重复过那道置位）。
		WireMaterial: true,
	})
	st.planFrameHash = digest
	return true
}

// RecordPlanFrameForLocked 在指定会话上收敛 plan 帧（调用方持有 Core.ViewMu）。
// 计划状态没有变化的调用是幂等的（内容哈希相同 → 一条不发）。
//
// 名字带 For 与其它按会话路由的方法同构；它同时是装配层的**兜底收敛口**：
// 计划状态被直接写进快照、没有可挂的写入点时，装配前调它补落一条。
func (c *Coordinator) RecordPlanFrameForLocked(sessionID string) bool {
	if c == nil {
		return false
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c.recordPlanFrameLocked(c.sessionStateLocked(sessionID))
}

// PlanFrameRecordedFor 返回指定会话最后一条 plan 帧的正文（供探针/审计读取）。
func (c *Coordinator) PlanFrameRecordedFor(sessionID string) (model.TranscriptEvent, bool) {
	if c == nil {
		return model.TranscriptEvent{}, false
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	transcript := c.sessionStateLocked(sessionID).transcript
	for index := len(transcript) - 1; index >= 0; index-- {
		if isPlanFrameEvent(transcript[index]) {
			return transcript[index], true
		}
	}
	return model.TranscriptEvent{}, false
}

// planFrameUnit 判定 plan 帧事件是否自成一格（协议单元）。
//
// 与激活技能正文同一姿势：帧在 wire 上是 system 角色（见
// providerRoleForTranscriptEvent），既不是用户提问、也不该把下一轮的 assistant
// 消息卷进自己的单元——它只是"这一轮开始前的状态材料"。
func planFrameUnit(event model.TranscriptEvent) bool {
	return isPlanFrameEvent(event)
}
