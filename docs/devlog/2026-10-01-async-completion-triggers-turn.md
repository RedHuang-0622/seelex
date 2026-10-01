# 后台作业终态触发对话（2026-10-01）

- 范围：`application/core/async_completion.go`（新）、`application/core/work_table.go`
  （信号消费者）、`seelexctx/limits.go` + `config/seelex.yaml` +
  `internal/bootseed/assets/config/seelex.yaml`（开关）、`scripts/gen_core_readme_index.py`
  （新文件归卷）、`application/core/async_completion_trigger_test.go`（新）
- 用户口径（原文）：「先提交再做 task 完成（done 和 fail 都触发）后触发对话的功能 feat」——
  即作业完成时该自己把回合起起来，不必等用户再敲一句。

## 0. 缺口是什么

作业面从设计起就是**轮询型**：`bash_bg` / `read_batch` / `subagent` 派发即结束，结果由模型
自己的下一次工具调用 `job_manage(op=fetch)` 取回；模型答完就停，**本轮之内没有唤醒链路**
（`docs/2026-09-24-async-tool-deferred-ack/README.md` §10.5 已把它记成明账：「跨回合无人
取回：打点块缓解、不消除」）。现场症状：一条跑十分钟的后台命令完成后，除非用户再发一句话，
否则没人取结果、也没人继续干活。

本次补的就是那一步：**作业落到终态 → 为它所属会话起一个回合**。

## 1. 口径（写死，实现里不许漂）

| 维度 | 取值 | 理由 |
|---|---|---|
| 触发状态 | `done`、`failed` | 用户口径「done 和 fail 都触发」。`killed` 不触发——那不是"作业有了结果"，是被终止；`running` 不是终态。 |
| 会话状态 | **只在空闲时** | 铁律 §6.1「绝不唤醒忙会话」。忙会话不被打断、也不往它的队列里塞东西；它的下一次回合边界本来就会在请求尾部打点块里看到这条**完成行**（`work_table_async.go`）。两者互补：忙会话走打点块，空闲会话走这里。 |
| 幂等 | 每个 handle 一次 | 句柄 `a<seq>` 单调且永不复用，进程内一个集合就够。因会话忙而跳过的条目**刻意不记账**，下一次信号还会再试。 |
| 账本规模 | 被登记表封顶 | 每轮扫描后用"登记表当下还在的句柄"裁剪账本（被取回/销项/驱逐的句柄出账），不随进程存活时长无限增长。 |
| 落点 | 注册表里的会话 | 注册表里没有它 = 本进程没有回合落点（别的进程的作业归属 / 会话已删）。不能交给 `submitConversationFor`——它会经 `sessionUnitLocked` 按需建单元，等于凭一条作业记录复活陌生会话。 |
| 开关 | `limits.async_exec.trigger_conversation` | 默认关（零值），出厂配置打开。与 `async_exec` 同一套"关就是关、无静默降级"的纪律。 |

正文是一条**用户行**（走 `submitConversationFor` 的正常输入路径），因此自带 handle / kind /
state / exit / 有界摘要 / 有界标题与取回指令——字段口径与打点块同源，绝对路径、末行原文、
时间戳都不进上下文（见 `dto.AsyncRunRecord` 的字段分层）。

## 2. 踩到的坑（这条值得单独记）

第一版把它做成**第五个生命周期消费者**（自己 `go consumeAsyncCompletions()`，自己也调
`Runtime.AsyncRunEvents()`）。表现：**作业完成了，什么都没发生**，且完全确定。

根因：`AsyncRunEvents()` 是**容量 1 的信号口**，同一次发送只交给一个接收者。四个既有消费者
里的 `consumeAsyncRuns` 已经先等在它上面了（FIFO），后起的第五个消费者**一次也收不到**。

诊断路径（留档，别重复走）：加 `runChatDebug` 观察到

```text
[runChat-debug] async-trigger: consumer started ch=0xc0003f2700 stop=0xc0003f2a80
    async_completion_trigger_test.go:80: DIAG test channel 0xc0003f2700     ← 同一个通道
```

同一个通道、消费者活着（5 秒后收 shutdown 信号才退出）、发送没阻塞（容量 1 进缓冲）——
只剩"有另一个接收者"这一种解释。

**结论：信号是"有事发生"的广播，一个信号口只该有一个消费者；第二件事必须并进同一个消费者。**
实现因此收成：`consumeAsyncRuns` 收到信号先重投影工作表格，再（开关打开时）跑一次终态扫描。

> 附带效果：这也解释了为什么 busy 会话那个用例当时"偶尔能过"——那次发送恰好赶上
> `consumeAsyncRuns` 在忙（不在 select 里等待），发送便交给了唯一在等的接收者。**偶过的
> 用例比稳定的红更危险**，它把"单接收者"这件事藏起来了。

## 3. 验收

```text
go test ./application/core/ -run TestAsyncCompletion -count=1 -v      7/7 PASS
```

| 用例 | 钉住的事实 |
|---|---|
| `TestAsyncCompletionTriggersIdleSessionTurn` | done → 起回合；正文含 handle / 状态 / 取回指令，且不含路径与探针字段 |
| `TestAsyncCompletionTriggersOnFailureToo` | failed 也触发，且如实带 exit code |
| `TestAsyncCompletionIgnoresRunningAndKilled` | running / killed 都不触发（纯函数 + 投影面各一遍） |
| `TestAsyncCompletionDoesNotWakeBusySession` | 忙会话不被唤醒；**回到空闲后的下一次信号补上**（证明"不记账 = 留待重试"） |
| `TestAsyncCompletionTriggersOncePerHandle` | 同句柄只起一轮（记录还在册时再发信号不重复触发） |
| `TestAsyncCompletionStaysOffWhenDisabled` | 开关关闭时一次都不触发 |
| `TestAsyncCompletionIgnoresJobWithoutSession` | 无会话归属 / 会话不在注册表 → 不触发 |

## 4. 边界（未验证 / 未做）

- **端到端未在真机 GUI 上冒烟**：用例走的是 `fakeRuntime` 的作业投影与真实 `Service` 装配，
  真实 `bash_bg` 作业从派发到终态到起回合的整链只由生产路径的一致性推出，未逐操作点过。
- **忙会话的触发是"丢掉重试"，不是"排队"**：若某会话长期忙（例如一直有排队输入），这条作业
  可能一直等不到触发。这是刻意的（不 push 进忙会话），但代价要认。
- **一次爆发会触发多次**：一轮里派发 N 条作业、会话随即空闲，则 N 条终态各自起一轮。没有做
  合并/去抖——用户口径是"完成就触发"，合并是另一个产品取舍。
