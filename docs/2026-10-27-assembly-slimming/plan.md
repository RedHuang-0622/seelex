# Assembly 瘦身与测试归位方案

## 批次

### A1：Conversation turn builder

- 将两阶段 turn 装配合并为 context 就绪后的一次装配。
- 11 个只被装配函数读取的 `conversationTurn*` 字段改为局部变量。
- `Service` 只保留 `chat.go` 实际消费的 Process 与 Runner。
- 删除对应字段存在性测试。

### A2：Edit fork builder

- Registry、Validator、Resolver、Rollback、Process 只在装配期存在。
- Annotation 仍由快照装配消费，单独判断是否保留或改由 builder 返回。

### A3：Session cold builder

- 九个 Cold stage 组合成一次性返回的冷恢复 pipeline。
- `Service` 只保留顶层调用所需的最终 owner。

### A4：剩余内部专用字段

- 逐字段确认调用次数和生命周期。
- 单次使用改 builder-local；共享锁/缓存/后台任务保留。

### A5：消费方生命周期

- 对只在 `ComponentsFor` 出现的无状态 owner 建立 factory。
- 由 `wiring/bootstrap` 决定实例所有权。
- 前端继续只声明窄接口，不 import owner 实现。

### T1：结构测试去重

- 合并小文件结构门禁。
- 删除字段存在性、反射字段类型和源码单行转发测试。

### T2：owner 测试迁出

- turn stage 行为测试迁 `application/conversation`。
- edit fork 行为测试迁 `application/conversation/edit`。
- cold stage 行为测试迁 `application/session/cold`。

### T3：公开 API 外部测试

- assembly 公开构造与 Components 测试使用 `package assembly_test`。
- application 根测试继续使用 `application_test`，合并重复的包边界检查。

### T4：默认套件分层

- probe / AB / benchmark 放入独立 build tag 或测试子包。
- 默认测试保留跨组件行为，不删唯一覆盖。

## 验收

- `serviceComponents` 字段数量持续下降。
- `assemble*` 方法按 owner 能力合并，而不是改名为新的 facade。
- assembly 测试文件数和默认套件行数下降。
- owner 行为测试总数不下降。
- `go test ./application/... ./gui ./tui ./headless ./console ./wiring/...`
- `go test ./e2e/... ./test/`
- `go build ./... && go build -tags "gui,desktop,production" ./...`
- `go vet ./...`

