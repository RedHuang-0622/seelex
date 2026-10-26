# Backend/Middle 外部端口迁移任务提示词

你负责为 Seelex 拆除 `application/core` 剩余的 backend/middle/store 直接依赖。
不要继续在 Application 内复制状态机，也不要把完整 `Service`、完整 Engine 或完整
Session state 包装成万能端口。目标是让真实消费方持有窄端口，由
`wiring/adapters` 注入唯一实现。

开始前必须阅读：

- `AGENTS.md`、`MEMORY.md`；
- `docs/2026-10-25-application-modularization/plan.md`；
- `docs/2026-10-25-application-modularization/implementation.md`；
- `docs/2026-10-25-application-modularization/port-migration-plan.md`；
- `application/README.md`、`application/core/README.md`；
- 目标 `backend/**`、`middle/**`、`store/**` 包的 `README.md` 和现有测试。

先执行 `git status`，保留并行会话和用户已有改动。不得回滚、清理或覆盖无关文件。
不得运行 clean、rebuild、release、`git reset --hard` 或 `git checkout --`。
提交时必须按仓库要求处理危险操作；如自动 post-commit build 会重建产物，使用
`SKIP_BUILD=1`。

## 当前基线

截至本提示词编写时：

- `Service` 公开方法：23；
- `application/core` 直接内部依赖：16；
- `application/core` 生产文件：117；
- `application/core` 生产代码：11,829 行；
- AP-116F2g、EP-001～EP-018 已完成。

开工前必须重新运行预算测试，不能只信文档中的数字。已完成且不要重做的端口：

- History Window；
- Residency；
- Conversation Stream；
- Account Window；
- Skill Activation；
- Session Records；
- Error Presentation；
- Context Retain Floor；
- Prompt Assets；
- Approval Broker factory；
- Input Router；
- Subagent Tool Event DTO；
- Team Role Sessions；
- Teamwork Board Reader；
- Assemble DTO / Host Session；
- Goal Coordinator；
- Middle Model alias；
- Middle View 只读模型。

## 剩余依赖分类

以 `rg` 的生产文件实测结果为准。当前大致分为四类：

1. Middle 视图与读取面：
   - `middle/view/board`
   - `middle/view/worktable`
   - `middle/view/state`
   - `middle/view/subagent`
2. Engine/Runtime：
   - `backend/engine/context`
   - `backend/engine/input/contextcontrol`
   - `backend/engine/prompt`
   - `backend/engine/session`
   - `backend/engine/task`
3. Limit 与会话用例：
   - `backend/limit`
   - `backend/limit/tier`
   - `middle/switch`
   - `middle/switch/sessionusecase`
   - `middle/switch/workspace`
4. Store 与事件：
   - `store`
   - `middle/protocol/event`

`middle/protocol/event` 先判断它是稳定协议形状还是可替换实现。只有真实调用面要求
替换时才定义端口；不要为了把 import 数降到零而复制 EventHub。

## 批次一：Middle 视图只读端口

优先处理不改变 Session/Conversation 状态所有权的只读面：

- WorkTable：行投影、批次、Plan/SubAgent 克隆、Task 状态映射、证据截断；
- Board：`ProjectBoardSink` 与可选 `TeamworkPlanSource`；
- State/Subagent：只搬读投影和纯构造，不搬 Coordinator 状态机。

先列每个函数/类型的真实调用方。可以按消费方定义最多 20 个方法的端口；adapter
只转发现有纯函数，不重新实现排序、去重、状态映射或截断规则。查询路径不得获取共享
写锁，也不得执行慢 I/O。

红测必须先证明 core 仍 import 目标包；实现后钉住：

- core import 归零；
- adapter 与 Middle 原函数同输入逐字段一致；
- WorkTable 行顺序、批次边界、证据截断和 Plan 依赖不漂移；
- ProjectBoard sink 的单方法语义和 nil 行为不漂移；
- GUI/TUI wire 与事件 revision/seq 不变。

## 批次二：Engine/Runtime 窄端口

按真实调用方拆成 Context、SessionRuntime、Task 和 Prompt 端口。只读路径先行：

1. history/transcript/input-index 读取；
2. Plan、Task 和 Provider history 投影；
3. Session runtime snapshot/materialization；
4. 最后才处理 restore、fork、submit、compact、cancel 等写路径。

禁止暴露完整 `Coordinator` 或 `SessionUnit`。端口只使用 `contract`、
`contract/dto`、`contract/protocol`、标准库或纯值类型。Engine 仍是状态机唯一所有者；
adapter 只做值映射与转发。所有慢读、压缩和磁盘访问必须位于 `ViewMu` 外。

必测：adapter parity、取消/超时、陈旧 request、restore/fork 顺序、慢 I/O 不持
`ViewMu`、race 和关闭路径。

## 批次三：Limit / SessionUseCase / Store

- Limit：先做 `Limits` 快照与 `tier` 纯值端口，再做 Knobs/Policy 写入；
- SessionUseCase：先完成 `SessionUnit`、View、Chat、Queue、Approval、Residency 的
  聚合归属裁决，再定义端口；不得镜像完整 session state；
- Store：按 SessionRepository、EventRepository、ToolResultRepository、
  BoardRepository 拆读面，key 必须显式含 projectID/sessionID；
- 档位词、状态词、上限表、事务、关闭、flush 和 LRU 判据仍只在原领域保留一份。

## 每批执行纪律

1. 写红测：import 门禁、结构断言或行为 parity；
2. 定义消费方端口和纯值 DTO；
3. 在拥有具体依赖的包实现或复用唯一 adapter；
4. 在 composition root 注入；
5. 迁移全部真实调用方；
6. 删除 core 的直接 import 和旧构造路径；
7. 更新目标包 README、Application README、`implementation.md` 和预算；
8. 跑受影响包、Application/Contract/E2E、Wiring、race、build、vet、
   README refs、`gofmt` 与 `git diff --check`。

每批生产迁移原则上不超过 2,000 行，公开新增方法尽量不超过 20 个。纯搬运和语义修改
分开提交。旧兼容入口只允许一行转发，不得出现第二份状态机。

## 停止条件

出现以下任一情况立即停止该批次并报告：

- 端口必须暴露完整 `Service`、完整 Engine 或完整 Session state；
- 需要复制 history、residency、permission、session 或 chat 状态机；
- DTO 不完整，必须把 Backend/Middle 具体类型放进公开端口；
- 锁顺序、revision/seq、事件顺序或恢复语义无法 parity；
- 端口没有真实调用方；
- 直接依赖数与生产代码没有净下降。

## 交付格式

每个批次报告：

1. 删除的直接 import；
2. 新增/修改的 DTO、interface、struct；
3. 唯一实现、adapter、首调用方；
4. 删除/合并的重复实现（`文件:行 → 新唯一位置`）；
5. core 直接依赖/文件/行数和 `Service` 方法数前后变化；
6. 红测失败证据、绿测命令和结果；
7. GUI/TUI wire、事件 revision/seq、持久化格式是否变化；
8. 未完成项、跨会话阻塞和回退点。
