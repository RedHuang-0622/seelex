# core/archive（根包分卷）

## 生态位

会话归档与收尾语义（归档隐藏/拒绝忙碌会话、推理与工具叙述随记录持久化、运行判定与取消排空）

覆盖：`archive*.go` + 显式名单（见生成器 `ROOT_GROUPS`）；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### archive_reasoning_persist_test.go

- `func TestPersistKeepsToolNarrationAndReasoningInRecord(t *testing.T)` — TestPersistKeepsToolNarrationAndReasoningInRecord：真实轨迹里工具轮之间

### archive_session.go

- `func (service *Service) ArchiveSession(sessionID string) error` — ArchiveSession 归档指定会话：非 running/queued/awaiting_approval 才允许；

### archive_session_test.go

- `func newArchiveCommandSessions() *archiveCommandSessions`
- `func (sessions *archiveCommandSessions) add(projectID, sessionID string)`
- `func (sessions *archiveCommandSessions) recordFor(projectID, sessionID string) (SessionRecord, bool)`
- `func (sessions *archiveCommandSessions) SaveSessionRecordWorkspace(projectID, sessionID string, record SessionRecord) error`
- `func (sessions *archiveCommandSessions) SaveSessionRecord(sessionID string, record SessionRecord) error`
- `func (sessions *archiveCommandSessions) LoadSessionRecordWorkspace(projectID, sessionID string) (SessionRecord, error)`
- `func (sessions *archiveCommandSessions) LoadSessionRecord(sessionID string) (SessionRecord, error)`
- `func (sessions *archiveCommandSessions) SessionsOf(projectID string) []SessionInfo` — SessionsOf 以 record 状态覆盖目录行（与生产 adapter 一致）。
- `func archiveTestService(t *testing.T, sessions *archiveCommandSessions) *Service`
- `func TestArchiveSessionHidesSessionFromItsProjectGrid(t *testing.T)` — TestArchiveSessionHidesSessionFromItsProjectGrid C2 契约：归档后 record
- `func TestArchiveSessionRejectsBusySessions(t *testing.T)` — TestArchiveSessionRejectsBusySessions C2 门控：running/queued/awaiting
- `func TestArchiveSessionMissingRecord(t *testing.T)` — TestArchiveSessionMissingRecord 钉住失败路径：会话不存在/record 缺失时

### close_semantics_test.go

- `func TestAnyChatRunningReflectsBackgroundSessions(t *testing.T)` — TestAnyChatRunningReflectsBackgroundSessions G0c 判定面：视图快照只镜像
- `func TestCancelAllChatsDrainsEveryRunningSession(t *testing.T)` — TestCancelAllChatsDrainsEveryRunningSession G0c 超时路径：取消必须覆盖
