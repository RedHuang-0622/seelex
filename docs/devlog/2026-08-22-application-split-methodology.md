# Application Core 拆分与清理方法论沉淀

日期：2026-08-22
范围：`application/core` 子包化（design.md S0–S4）、门面/兼容层清理、文件
分卷（≤1000 行）与 README 自动文档化。
验证证据：`go build ./...`、`-tags "gui,desktop,production"`、`go vet`、
`go test ./application/... ./internal/adapters/... ./e2e/...`、core `-race`、
`gofmt -l`、`git diff --check` 全绿；`e2e/layout_test.go` README 链接检查通过。

本文沉淀的是"大包拆分 + 收尾清理 + 文档化"的可复用方法，不是当前实现事实
（实现事实以代码与模块 README 为准）。

## 1. 大包拆分的落地顺序：内核先行，每步全绿

不要按文件直接搬（高耦合方法跨包必炸）。本次采用：

1. **共享内核先行**：把锁、权威 Snapshot、外部端口依赖收敛到
   `core/internal/state`（`Core{Mu, Snapshot, Deps, Events, Approval}`），
   根门面与各域协调器嵌入 `*state.Core`，域之间不再互相持有实现。
2. **机械改名 + 编译器兜底**：字段迁移（`mu→Mu` 等）先定"接收者白名单"
   （哪些接收者改、哪些保留），然后批量替换；漏改/误改全部由
   `undefined field` 编译错误暴露，逐个修复即可。注意白名单必须以**实际代码
   结构**校验——本次设计稿声称 `TaskService` 自持 `mu/snapshot`，实际它嵌入
   `*serviceState`，编译器才是最终检查表。
3. **消费方端口注入**：域包声明自己需要的窄接口（如
   `session_runtime.TaskPersistencePort`），实现方结构满足即实现，装配根注入，
   编译期断言固化（`var _ session_runtime.TaskPersistencePort = (*Coordinator)(nil)`）。
4. **先叶子后依赖**：先搬零依赖叶子（chat/worktable/input_router/
   context_control），再搬 session → task → view/prompt/subagent/context。
5. **每步验收**：`go build ./...` + 目标测试 + race 全绿再进下一步，步骤之间
   不叠加未验证改动。

## 2. 跨域纯函数的依赖方向：三个层次

拆包时最常卡住的是"小函数在根包、多个域都要用"：

- **能下沉就下沉**：进程级全局（`Limits()`）下沉为叶子包 `internal/limits`，
  根包保留门面包装，域包直接 import，根因是避免根包导入环。
- **该复用就直连**：纯函数（token 预算、transcript 收敛、Plan 投影）随
  归属域下沉后，兄弟域包**直接 import**（如 `context_runtime` 用
  `task_context.ContextBudgetFor`），只要无环且语义归属清晰。
- **边界模糊就端口注入**：跨域谓词/呈现函数（`isTaskContextCheckpoint`、
  `oversizedToolResultWarning`、`displayUserInput`）以 `Deps` 函数端口注入，
  装配根闭包接线，保持"域包只依赖消费方窄接口"。

判据：能明确归属 → 下沉直连；会制造环或反向依赖 → 端口注入。

## 3. 兼容垫层（compat/facade）的生命周期

迁移期保留根包兼容包装（`task_context_compat.go`、`context_runtime_compat.go`、
`subagent_facade.go`）可以压低单步改动面，但它们是**临时脚手架**，收尾阶段
必须清掉：

- 清理解除条件：包装只剩"转发"，无额外语义（错误码包装等真语义要内联保留）。
- 清理方式：全局搜索每个包装符号的引用面 → 直接改写调用点
  （`task_context.X`/`context_runtime.X`）→ 删除垫层文件。
- 保留底线：`application` 门面导出符号（`Service` 公开方法）不变，只允许
  改实现路径，不允许删 API。

## 4. 机械重构的安全带

- 每次批量改名/搬移后：`gofmt -l`、`go build ./...`、`go vet`、目标测试、
  `go test -race`、`git diff --check`。
- 编译器是"完整检查表"：删字段/删符号后，所有漏网引用都会以编译错误出现。
- README 链接完整性由 `e2e/layout_test.go` 固化（模块必须有 README、链接必须
  存在）——文档化改动后必须重跑 e2e。
- 行尾差异：Windows 工作树 CRLF + `core.autocrlf=true`，`gofmt -w` 规范化
  不会产生 git 噪音；`git diff --check` 输出里的 "CRLF will be replaced"
  是正常警告，不是错误。

## 5. 文件分卷与测试拆分

- 硬约束：单文件 ≤ 1000 行；拆到 800 行以下留余量。
- 测试文件同样受约束；按"夹具/fakes"与"用例"、或按测试域拆。
- 脚本化拆分要点：按**包名引用**自动分配 import（解析每个拆分块的 `pkg.`
  使用，只保留用到的 import），避免手工维护两组 import。

**事故教训（本次发生）**：拆分脚本把第二部分写到与原文件同名路径，随后
`unlink` 原路径时误删了刚写好的文件。规则：**写入必须走临时文件 + `os.replace`，
且删除目标路径与写入目标路径不得同名**。误删后恢复手段：依据其他测试文件的
使用点（字段引用、channel 名、行为断言）重建夹具/helper，再靠全量测试验证
行为一致。

## 6. 文档与代码同源：README 自动生成

- 函数说明的**唯一事实源是源码 doc 注释**；README 的"文件与函数索引"由脚本
  `scripts/gen_core_readme_index.py` 从注释提取（首行摘要），改注释即刷新
  文档，杜绝两处漂移。
- 根包按文件前缀分卷（`README-service.md`/`README-session.md`/...），主
  README 只留生态位与导航；叶子包单 README（生态位 + 索引）。
- 语言统一：仓库默认中文注释（代码标识符保留原文）；本次把 24 处英文 doc
  注释翻译为中文，使 README 索引全中文。完整 README 编写规范见
  `docs/arch/readme-spec.md`（AGENTS.md 已引用，后续模块 README 统一遵循）。

**编码陷阱（本次发生）**：在 PowerShell 里用 `@'...'@ | python -` 传脚本，
管道编码会把非 ASCII 字符替换成字面 `?`，导致生成的 README 里真写入
`## ???????`。规则：**凡脚本含中文，必须落盘为 UTF-8 文件再执行**；验证时
同样用文件脚本或 unicode 转义，不要在管道里内联中文断言。

## 7. 误删与恢复的通用方法

源码文件被误删且未提交时：

1. 先查仓库内是否有一致实现（同款 helper 的定义/复制品）。
2. 从所有**使用点**反向重建：签名从调用处推断，行为从断言/字段引用推断
   （channel 名、触发顺序、返回值语义）。
3. 用全量测试验证重建等价，不猜测关键语义。

## 8. 适用边界

上述方法适用于"已有代码库内部重组 + 文档化"，目标是行为零变化。涉及对外
契约（API/持久化格式/协议）变更时，必须叠加契约测试与迁移路径，不能只靠
重构安全带。
