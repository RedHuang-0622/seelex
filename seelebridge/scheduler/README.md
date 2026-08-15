# scheduler 域

## 模块定位

承载 seelex 的定时周期任务 actor：标准库 time.Ticker 驱动单循环 goroutine，
任务两类（command 白名单命令 / prompt 复用 agent 会话）。主要调用方：
`ports.go`（调度器端口）、`main.go`（白名单与执行器装配）。

## 职责与非职责

- 职责：任务登记/校验/排期/取消/快照、白名单命令执行（argv 直传、环境
  清洗、超时）、prompt 任务委托执行器。
- 非职责：会话执行本身（归 session/agent）、命令的 shell 语义。

## 与其它域的关系

```text
runtime/ports.go ──► scheduler.State ──► security（ScrubEnvironment）
     │
     └──► application Submit（经 PromptExecutor 闭包注入）
```

## 核心实现

- `State`：自带锁的 actor；ticker 循环 `tick` 找出到期任务，独立 goroutine
  执行（running 标志防重叠），状态快照只读外发。
- `Schedule`：校验周期下限/周期单位（`period_unit`）+ 数值/命令白名单/prompt
  执行器装配后创建任务；周期表达支持 hour/day/week/month，month 为日历月
  （`addCalendarMonths` 月末钳制），无单位时回退 `Interval` 秒级固定周期。

## 数据流

Schedule → start（惰启动 ticker）→ tick → executeTask（下次运行按
`nextScheduledAt` 计算）→ 状态回写 + observe（通知 application 投影）。

## 依赖方向

允许依赖：`dto`、`security`。禁止依赖：seelebridge 根包及其它域。

## 并发、存储、安全

单循环 goroutine + 每任务独立执行 goroutine；环境变量经
`security.ScrubEnvironment` 清洗；白名单命令 argv 固定直传不经 shell。

## 扩展方式

新增任务类型：扩展 `Schedule` 校验与 `executeTask` 分支。

## Review 指南

- 同一任务是否可能重叠执行；停机是否等待运行中任务（schedulerShutdownWait）。

## 测试与验证

`go test ./seelebridge/scheduler/...`（scheduler_test.go 随域迁移）。
