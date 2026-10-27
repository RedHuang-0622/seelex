# Assembly 职责与测试瘦身前置审查

日期：2026-10-27
起点：`e5d09213`

## 目标

1. 清除只用于单次装配、随后只被装配函数读取的 `serviceComponents` 占位字段。
2. 将只服务单一 owner 的行为测试迁到 owner 包。
3. 将只验证公开装配面与结构边界的测试迁到 `package assembly_test`。
4. 根 `application` 的包边界测试按同一口径合并去重。
5. 保持锁序、session/project scope、revision/seq、持久化和关闭顺序不变。

## 当前基线

| 指标 | 当前 |
|---|---:|
| assembly 生产文件 | 54 |
| assembly 生产代码 | 6,613 行 |
| `serviceComponents` 字段 | 102 |
| assembly 内部专用字段 | 54 |
| `assemble*` 方法 | 39 |
| assembly 测试文件 | 230 |
| assembly 测试代码 | 39,240 行 |
| assembly `Test*` | 862 |
| application 根测试文件 | 84 |
| application 根测试代码 | 5,065 行 |

## 第一批结果（2026-10-27）

| 指标 | 起点 | 现在 | 变化 |
|---|---:|---:|---:|
| `serviceComponents` 字段 | 102 | 77 | -25 |
| assembly 生产代码 | 6,613 | 6,586 | -27 |
| assembly 测试文件 | 230 | 211 | -19 |
| assembly 测试代码 | 39,240 | 37,031 | -2,209 |
| application 根测试文件 | 84 | 67 | -17 |
| application 根测试代码 | 5,065 | 4,784 | -281 |

本批把 turn 行为测试迁到 `application/conversation` 的 `conversation_test`，
并在 assembly 增加 `package assembly_test` 公开 API 测试。删除的是字段存在性、
源码单行转发和重复路径断言；行为测试保留并换到 owner 包。

## 第二批结果（2026-10-27）

| 指标 | 第一批结束 | 第二批结束 | 变化 |
|---|---:|---:|---:|
| `serviceComponents` 字段 | 77 | 65 | -12 |
| assembly 生产文件 | 54 | 52 | -2 |
| assembly 生产代码 | 6,586 | 6,926 | +340* |
| assembly 测试文件 | 211 | 192 | -19 |
| assembly 测试代码 | 37,031 | 37,382 | +351* |
| application 根测试文件 | 67 | 67 | 0 |
| application 根测试代码 | 4,784 | 5,097 | +313* |

\* 前一批数字由 PowerShell `Measure-Object` 统计，本批改用与 Go 预算测试一致的
`sourceLineCount`（按换行计），因此行数口径更严格，不能与本表左列直接相减。
文件数和结构字段数是可比口径。

第二批把 edit-fork、cold-stage、draft 与 fork-lineage 的真实行为测试迁到 owner
外部测试包；删除了旧常量死文件、字段反射断言和纯转发测试。assembly 生产/测试、
组件字段和默认测试规模已写入只许收紧的预算。

## 分类

### A. 单次 builder 中间件

- `conversationTurn*`：13 个字段，实际只有 Process 与 Runner 被 `chat.go` 使用。
- `editFork*`：内部 Registry、Validator、Resolver、Rollback、Process、Annotation。
- `sessionCold*`：九个恢复阶段只用于构造冷恢复流水线。

这些不是消费方依赖，不应成为 `Service` 的长期状态。

### B. 组装后只暴露给消费方

约 20 个 Application owner 只在装配处和 `ComponentsFor` 出现，没有 assembly 内部第二消费者。
它们可以逐步改为 composition root / factory 构造，但不能让前端直接 import 具体 owner。

### C. 必须共享生命周期的协调器

`view`、`sessions`、`tasks`、`residency`、`workTableRuntime`、`sessionHistoryRuntime`
和 conversation turn state 被 assembly 多处使用，并承载锁、缓存、revision 与后台任务。
这些不能交给单个前端控制生命周期。

### D. 测试分类

- 纯字段存在/源码单行转发断言：删除或合并为结构门禁。
- 只测一个 owner 的算法/失败语义：迁 owner 包。
- 只使用 assembly 公开 API：改为 `package assembly_test`。
- 跨组件锁序、scope、恢复、revision：保留在 assembly。
- AB/probe/benchmark：迁独立 probe build 或独立包，默认套件不加载。

