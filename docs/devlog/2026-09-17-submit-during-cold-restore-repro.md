# 2026-09-17 运行中会话切到冷会话：输入落进「恢复中」空壳（复现与根因）

## 1. 现象（用户报告）

> 在一个会话运行中，从另外一个不在运行中的会话发送消息，会因为一些问题发不出去。
> 怀疑是「输入区锁」和「视图指针」。

## 2. 复现（应用层，确定性；RED 现场保留在本文档）

把下面这段探针临时放进 `application/core/`（`newTestService` / `gatedHistorySessions` /
`newMultiSessionEngine` 都是既有测试夹具）：

```go
func TestProbeSubmitDuringAsyncColdRestore(t *testing.T) {
	engine := newMultiSessionEngine()
	now := time.Now()
	gated := &gatedHistorySessions{
		scopedSessions: &scopedSessions{
			catalog: map[string][]SessionInfo{"": {
				{ID: "sess-cold", Name: "cold B", UpdatedAt: now},
				{ID: "sess-b", Name: "hot C", UpdatedAt: now},
			}},
			histories: map[string]map[string][]EngineMessage{"": {
				"sess-cold": {{Role: "user", Content: "cold b content"}},
				"sess-b":    {{Role: "user", Content: "hot c content"}},
			}},
		},
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	service := newTestService(t, engine, withTestSessions(gated))
	defer service.Shutdown()
	t.Cleanup(gated.releaseNow)

	// ① A 运行中
	if err := service.Submit(context.Background(), "task in flight"); err != nil {
		t.Fatalf("submit A: %v", err)
	}
	aID := engine.SessionID()
	waitChatStarted(t, engine.started[aID])

	// ② 冷会话 B 的存储读上门闩（模拟大会话冷加载）
	gated.armMu.Lock()
	gated.armed = true
	gated.armMu.Unlock()
	if err := service.ResumeSession("sess-cold"); err != nil {
		t.Fatalf("ResumeSession(cold B): %v", err)
	}
	<-gated.entered
	shell := service.Snapshot()
	t.Logf("shell: id=%s status=%s chat.running=%v conv=%d",
		shell.Session.ID, shell.Session.Status, shell.Chat.Running, len(shell.Conversation))

	// ③ 用户在「界面还在 A / B 还没装载完」的窗口里敲了一句话并回车
	err := service.Submit(context.Background(), "typed while B is still restoring")
	t.Logf("Submit during restoring => err=%v", err)

	// ④ 释放门闩，看装载完成后视图变成什么
	gated.releaseNow()
	time.Sleep(300 * time.Millisecond)
	final := service.Snapshot()
	t.Logf("final: id=%s status=%s chat.running=%v", final.Session.ID, final.Session.Status, final.Chat.Running)
	for i, m := range final.Conversation {
		t.Logf("  conv[%d] %s %q", i, m.Role, m.Content)
	}
	close(engine.release[aID])
}
```

实测输出（`go test ./application/core/ -run TestProbeSubmitDuringAsyncColdRestore -v`）：

```text
A=draft_..._1 running
shell: id=sess-cold status=restoring chat.running=false conversation=0
Submit during restoring => err=<nil>
after submit: view id=sess-cold status=restoring chat.running=true conversation=2
engine streamCalls: A=1 cold=0 | history len: A=2 cold=0
after cold load completes: view id=sess-cold status=running chat.running=true conversation=3
  conversation[0] role=user      content="typed while B is still restoring"
  conversation[1] role=assistant content=""
  conversation[2] role=user      content="cold b content"
```

即：**「恢复中」空壳期间提交的输入被应用层正常接受（err=nil），并在还没装载完的目标会话
上开了一个真回合**；A 的引擎调用数不变（没有落进 A、也没进 A 的队列）。

## 3. 链路（根因）

1. **切换的三分支**（`application/core/session_history.go:30-93`）：
   目标已驻留 → 热挂载；目标未驻留且**无**会话运行 → 同步冷加载；目标未驻留且**有**会话
   运行 → **异步冷加载**。
2. **异步分支先动视图指针**（`session_history.go:74-93` → `beginAsyncRestore`
   `:124-158`）：`Core.Snapshot.Session = {ID: 目标, Status: restoring}`（`:151`）
   并 `publishSessionChanged`，装载丢给后台 goroutine；RPC 立刻返回。也就是说**视图指针
   已经指向 B，而 B 的内容还没读回来**。
3. **输入区锁是前端唯一的一道锁**（`gui/frontend/dist/chat-view.js:14-22`）：
   `snapshot.session?.status === "restoring"` → `prompt.disabled = true`、
   `send-button.disabled = true`、placeholder「会话内容恢复中」。语义在
   `gui/frontend/README.md:136` 与 `application/core/session_scope.go:239-251` 的注释里
   写得很清楚：「渲染层停在 restoring 空壳（**输入区禁用、消息发不出去**）」。
4. **应用层没有对应的门**：`submitConversation`（`application/core/service_input.go:82-131`）
   与 `startChatFor`（`application/core/chat.go:48-63`）只按
   `currentViewSessionID()` / `Snapshot.Session.ID` 路由，**不检查目标会话是否正在
   restoring**。而这两个值在异步冷加载期间已经等于目标。
   ⇒ 「后端视图指针已切到 B」到「前端渲染到 restoring 快照」之间存在一个窗口；窗口里
   提交的输入会被开在空壳 B 上（实测 ③）。
5. **次生后果：恢复快照不再安装**。`resumeSessionCold` 的基地安装有守卫
   「后台装载只在目标可见会话仍为空时才安装」（`session_history.go:456-462`，2026-09-11
   TC-A2-01 第三层根因的修复）。用户抢跑一轮后可见会话不再为空 → 装载完成后的恢复快照
   **不安装**，用户看到的是「新消息在前、被恢复的历史挂在后面」（实测 ④
   `conversation[2]` 落在新回合之后）。
6. **同类的粘滞锁路径（此前已修，有回归）**：后台冷加载**失败**时，
   `handleColdRestoreFailure`（`session_history.go:179-200`）异步把视图挂回切换前会话并发布
   进程级 `view.session.changed`；若 Bridge 的订阅键仍钉在失败目标上，该订阅此后永远沉默，
   渲染层就**永久**停在 restoring 空壳（输入区禁用、消息发不出去），且「再点回原会话」也
   因前后会话相同而跳过重订阅。修复与两个回归用例见 `gui/bridge_view_drift_test.go`
   （`TestBridgeSelfHealsSubscriptionAfterAsyncViewRollback`、
   `TestBridgeSameSessionRetryAfterRollbackHealsSubscription`）。

## 4. GUI 侧（computer use）观察

- 现场运行中的会话（会话树带 **运行中** 徽标）在屏；切到空闲会话后 composer 显示「就绪」，
  可正常输入并发送：`@goal-a2a` 提交成功、回执「已召唤团队 goal-a2a：3 个席位在编」落在
  **被切到的那个会话**里。⇒ **普通路径不丢字**，报告里说的「有时候」不是每次都发生。
- 被判为「冷」的两个大会话（hover 显示 310456 / 530523 tokens）切换后 composer 在 ~1s 内就
  回到「就绪」：异步冷加载只读**尾部窗口**，restoring 窗口太短，肉眼抓不到「会话内容恢复中」。
  这解释了症状的间歇性——**只有装载慢或失败时，锁才可见/持续**。
- 未能现场抓到锁定时截图；上面的应用层探针是确定性复现（同一份夹具、同一分支）。

## 5. 建议修法（已在 [2026-09-17-submit-during-cold-restore-fix.md](2026-09-17-submit-during-cold-restore-fix.md) 落地）

> 落地记录、锁竞争数据流（现状 → 修复后）与红绿验证见上述 fix 文档；本节保留当时的
> 原始建议文本，便于对照「建议 → 实现」的取舍。

1. **前端**：把「正在切换会话」也纳入输入区锁——`state.resumingSessionID` 非空时同样
   disable prompt / send（现在只 disable 目标行按钮），让锁覆盖「后端已切、前端还没渲染
   restoring」的窗口。改动最小、不触碰语义。
2. **应用层**：在 `submitConversation` / `submitConversationFor` 入口加 restoring 门——视图
   会话在 restoring 中时，要么明确拒绝（给可展示错误），要么挂到该会话装载完成点再启动
   （等于「等装载完再发」）。这是治本的一条：把「前端锁」升级为「后端不变量」。
3. **顺带**：`resumeSessionCold` 的基地安装守卫在「用户抢跑」时静默丢历史，建议改为
   「合并 / 补插」而不是静默不安装。

## 6. 判据不变式（建议写进设计）

> 会话处于 `restoring` 时，**应用层不得接受任何对话输入**（拒绝或延后），
> 不得依赖前端 `prompt.disabled` 作为唯一防线。
