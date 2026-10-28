# store/media（根包分卷）

## 生态位

定时任务事件等派生通道

覆盖：`schedule_events*.go`；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### schedule_events.go

- `func (store *storeEngine) scheduleEventCommit(key Key, kind structuralEventKind, payload ScheduleEventPayload) error` — scheduleEventCommit 写一条 schedule.* EVENT（唯一写序 = message → event）。
- `func (router *Router) ScheduleRegisterWorkspace(projectID, sessionID string, payload ScheduleEventPayload) error` — ScheduleRegisterWorkspace 写 schedule.registered EVENT（冷启动重建 timer）。
- `func (router *Router) ScheduleCancelWorkspace(projectID, sessionID string, payload ScheduleEventPayload) error` — ScheduleCancelWorkspace 写 schedule.cancelled EVENT。
- `func (router *Router) ScheduleFireWorkspace(projectID, sessionID string, payload ScheduleEventPayload) error` — ScheduleFireWorkspace 写 schedule.fired EVENT（审计；timer 实际触发由运行期处理）。
- `func (router *Router) scheduleEvent(projectID, sessionID string, kind structuralEventKind, payload ScheduleEventPayload) error`
