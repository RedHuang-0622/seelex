# test/probe（集成用例分卷）

## 生态位

一次性探针：headless 恢复前缀顺序与作业迁移残留（现场取证用，保持可复跑）。

覆盖：`headless_restore_prefix_probe*.go`、`job_migration_residue*.go`；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### headless_restore_prefix_probe_test.go

- `func newPrefixRecordingServer(t *testing.T) *prefixRecordingServer`
- `func (recorder *prefixRecordingServer) reset()`
- `func (recorder *prefixRecordingServer) snapshot() [][]probeWireMessage`
- `func (recorder *prefixRecordingServer) serve(t *testing.T, writer http.ResponseWriter, request *http.Request)`
- `func TestHeadlessRestorePrefixProbe(t *testing.T)` — TestHeadlessRestorePrefixProbe 是“恢复前缀一致性”的 headless 复现：
- `func runSettledRounds(t *testing.T, ctx context.Context, app interface { Submit(context.Context, string) error WaitForIdle(context.Context) error }, rounds int)`
- `func findRolloutFiles(root string) []string`
- `func assertSessionStorageEvidence(t *testing.T, storeRoot string)`
- `func findV8SessionDirs(t *testing.T, storeRoot string) []string`
- `func assertSessionStorageContainsKind(t *testing.T, storeRoot, wantKind string)`
- `func visiblePairMessages(messages []application.Message) []probeWireMessage` — visiblePairMessages 从快照可见会话里提取 user/assistant 对（跳过 system
- `func compareWirePrefix(got, want []probeWireMessage) []string`

### job_migration_residue_test.go

- `func TestRetiredJobToolNamesLeaveNoResidue(t *testing.T)` — TestRetiredJobToolNamesLeaveNoResidue 是打点 L-2/L-4/L-7 的落地判据（TC-L2-3 /
