import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const source = await readFile(new URL("./snapshot-shape.js", import.meta.url), "utf8");
const shape = await import(`data:text/javascript;base64,${Buffer.from(source).toString("base64")}`);

// 与 application/core/session_snapshot_test.go 同构的传输形状夹具（JSON 键
// 与 model DTO tag 一致）。
function sessionArtifact() {
  return {
    protocol_version: 1,
    revision: 9,
    session: { id: "sess-a", name: "A" },
    conversation: [{ id: "m1", role: "assistant", content: "hi" }],
    chat: { running: false },
    task: null,
    runtime: {
      effort: "medium",
      full_access: false,
      tokens: "120",
      replan: { in_flight: 0 },
      plan: null,
      work_table: []
    },
    capabilities: { session_resume: true, session_snapshot: true },
    history_offset: 0,
    total_messages: 1,
    has_more_history: false,
    conversation_window: 50,
    read_files: []
  };
}

function processArtifact() {
  return {
    protocol_version: 1,
    revision: 2,
    sessions: [{ id: "sess-a", name: "A", updated_at: "2026-09-03T00:00:00Z", token_count: 0 }],
    workspaces: [],
    session_workspaces: { "sess-a": "proj-a" },
    current_workspace: { id: "proj-a", name: "P", root_path: "G:/p" },
    capabilities: { session_resume: true },
    runtime: {
      model: "gpt-5",
      provider: "openai",
      account: "default",
      plugin: "",
      visible_tools: [{ name: "bash", description: "run" }],
      skills: [],
      plugins: [],
      accounts: [{ name: "default", provider: "openai", model: "gpt-5" }],
      scheduled_tasks: [],
      scheduled_commands: []
    }
  };
}

function workbenchArtifact() {
  return {
    protocol_version: 1,
    revision: 12,
    session: { id: "sess-a", name: "A" },
    sessions: [{ id: "sess-a", name: "A", updated_at: "2026-09-03T00:00:00Z", token_count: 0 }],
    conversation: [{ id: "m1", role: "assistant", content: "hi" }],
    chat: { running: false },
    task: null,
    runtime: {
      model: "gpt-5",
      provider: "openai",
      account: "default",
      effort: "high",
      full_access: false,
      tokens: "300",
      replan: { in_flight: 0 },
      plan: null,
      visible_tools: [{ name: "bash", description: "run" }],
      skills: [],
      plugins: [],
      accounts: [{ name: "default", provider: "openai", model: "gpt-5" }],
      scheduled_tasks: [],
      scheduled_commands: []
    },
    capabilities: { session_resume: true },
    session_workspaces: { "sess-a": "proj-a" },
    workspaces: [],
    current_workspace: null
  };
}

test("runtime ownership tables mirror the G3 model split without overlap", () => {
  // target-design §2.4：effort/fullAccess/tokens/replan/plan/todo/subagent
  // 树/激活 skill/worktable 属于会话；model/provider/account/plugin/能力
  // 清单/定时任务属于进程。
  for (const key of ["effort", "full_access", "permission_tier", "tokens", "replan", "plan", "todo_items", "subagent_tree", "goal_skill_active", "goal_governance", "active_skills", "work_table", "work_table_batches", "teamwork_board"]) {
    assert.ok(shape.SESSION_RUNTIME_KEYS.includes(key), `session runtime must own ${key}`);
  }
  for (const key of ["model", "provider", "account", "plugin", "permission_tiers", "visible_tools", "skills", "plugins", "accounts", "scheduled_tasks", "scheduled_commands"]) {
    assert.ok(shape.PROCESS_RUNTIME_KEYS.includes(key), `process runtime must own ${key}`);
  }
  for (const key of shape.SESSION_RUNTIME_KEYS) {
    assert.ok(!shape.PROCESS_RUNTIME_KEYS.includes(key), `${key} must not be owned twice`);
  }
  for (const key of shape.PROCESS_RUNTIME_KEYS) {
    assert.ok(!shape.SESSION_RUNTIME_KEYS.includes(key), `${key} must not be owned twice`);
  }
});

test("splitRuntime routes every field by ownership and keeps unknown keys", () => {
  const runtime = {
    model: "gpt-5",
    account: "default",
    effort: "lite",
    plan: { status: "running" },
    work_table: [{ id: "plan:n1" }],
    visible_tools: [{ name: "bash" }],
    future_field: "preserved"
  };
  const { session, process } = shape.splitRuntime(runtime);
  assert.deepEqual(session, { effort: "lite", plan: { status: "running" }, work_table: [{ id: "plan:n1" }], future_field: "preserved" });
  assert.deepEqual(process, { model: "gpt-5", account: "default", visible_tools: [{ name: "bash" }] });
  assert.equal(shape.sessionRuntimeOf(runtime).model, undefined);
  assert.equal(shape.processRuntimeOf(runtime).effort, undefined);
});

test("classifySnapshot distinguishes workbench/session/process artifacts", () => {
  assert.equal(shape.classifySnapshot(workbenchArtifact()), "workbench");
  assert.equal(shape.classifySnapshot(sessionArtifact()), "session");
  assert.equal(shape.classifySnapshot(processArtifact()), "process");
  assert.equal(shape.classifySnapshot({ protocol_version: 1 }), "unknown");
  assert.equal(shape.classifySnapshot(null), "unknown");
});

test("assertTypedShape rejects cross-field leaks (INV-G1 frontend mirror)", () => {
  // 会话制品携带进程顶层/运行字段 → 拒绝（sessions 会使形状退化为
  // workbench 联合快照，泄漏校验只对明确的会话制品生效）。
  assert.throws(
    () => shape.assertTypedShape({ ...sessionArtifact(), session_workspaces: {} }),
    /泄漏进程字段: session_workspaces/
  );
  assert.throws(
    () => shape.assertTypedShape({ ...sessionArtifact(), runtime: { ...sessionArtifact().runtime, model: "gpt-5", accounts: [] } }),
    /泄漏进程字段: model, accounts/
  );
  // 进程制品携带会话字段 → 拒绝。
  assert.throws(
    () => shape.assertTypedShape({ ...processArtifact(), chat: { running: false } }),
    /泄漏会话字段: chat/
  );
  assert.throws(
    () => shape.assertTypedShape({ ...processArtifact(), runtime: { ...processArtifact().runtime, effort: "high", plan: null } }),
    /泄漏会话字段: effort, plan/
  );
  // workbench 联合快照两字段都允许（分型前的桌面传输形状）。
  assert.equal(shape.assertTypedShape(workbenchArtifact()), "workbench");
  // 干净的会话制品与进程制品通过。
  assert.equal(shape.assertTypedShape(sessionArtifact()), "session");
  assert.equal(shape.assertTypedShape(processArtifact()), "process");
  // 无法识别形状拒绝。
  assert.throws(() => shape.assertTypedShape({ protocol_version: 1 }), /无法识别/);
});

test("processContextOf preserves the desktop process segment across session-scoped loads", () => {
  // 桌面启动收到 Workbench 联合快照：进程上下文（目录 + 进程运行原件）可整
  // 份提取，供后续会话粒度载荷不丢账户/插件/技能/定时任务。
  const context = shape.processContextOf(workbenchArtifact());
  assert.deepEqual(context.sessions, workbenchArtifact().sessions);
  assert.deepEqual(context.session_workspaces, { "sess-a": "proj-a" });
  assert.equal(context.runtime.model, "gpt-5");
  assert.equal(context.runtime.accounts.length, 1);
  assert.equal(context.runtime.visible_tools.length, 1);
  assert.equal(context.runtime.scheduled_commands.length, 0);
  // 进程上下文不含会话运行原件。
  assert.equal(context.runtime.effort, undefined);
  assert.equal(context.runtime.work_table, undefined);

  // 会话粒度载荷本身没有进程字段：进程上下文由宿主保留，不随会话载荷抖动。
  const sessionContext = shape.processContextOf(sessionArtifact());
  assert.deepEqual(sessionContext.runtime, {});
  assert.equal(sessionContext.sessions, undefined);
  // 渲染时把保留的进程运行段与会话运行段合并回联合 runtime（桌面现状口径）。
  const mergedRuntime = { ...shape.sessionRuntimeOf(sessionArtifact().runtime), ...context.runtime };
  assert.equal(mergedRuntime.effort, "medium");
  assert.equal(mergedRuntime.model, "gpt-5");
});

test("process snapshot carries process originals and no conversation payload", () => {
  const artifact = processArtifact();
  const raw = JSON.stringify(artifact);
  for (const forbidden of ["conversation", "chat", "effort", "work_table", "subagent_tree"]) {
    assert.ok(!raw.includes(`"${forbidden}"`), `process snapshot must not carry ${forbidden}`);
  }
  for (const required of ["sessions", "session_workspaces", "visible_tools", "accounts", "scheduled_tasks", "scheduled_commands"]) {
    assert.ok(raw.includes(`"${required}"`), `process snapshot must carry ${required}`);
  }
});

// 用户现场（2026-10-01）：「上下文压缩总是污染前端，然后在新开会话的时候带到新建
// 会话」——压缩记录挂在 task.context_compactions 上，而 task 是宿主侧随视图会话
// 切换重写的镜像。草稿会话（新建会话）结构上没有任务面，渲染层因此必须拒收快照里
// 残留的 task/read_files，而不是把上一个会话的压缩栈说成当前会话的。
test("stripDraftSessionFacts drops session facts a draft session cannot own", () => {
  const draft = sessionArtifact();
  draft.session = { id: "draft_1_1", name: "新会话", draft: true, status: "draft" };
  draft.task = {
    request_id: "task-from-previous-session",
    status: "progressing",
    context_compactions: [{ version: 3, reason: "context_budget", frame_ref: "tr-abc", message_to: "message-522" }]
  };
  draft.read_files = [{ path: "application/core/session_draft.go", read_at: "2026-10-01T00:00:00Z" }];

  const cleaned = shape.stripDraftSessionFacts(draft);
  assert.equal(cleaned.task, undefined, "草稿会话不得带任务面（压缩记录挂在它上面）");
  assert.equal(cleaned.read_files, undefined, "草稿会话不得带上一个会话的已读文件");
  assert.equal(cleaned.session.id, "draft_1_1");
  assert.equal(cleaned.conversation.length, 1, "只清会话事实，不动其它字段");
  // 不改入参：快照对象由 client-state 共享，就地删字段会让进程段合并读到半份快照。
  assert.ok(draft.task, "入参不得被修改");
});

test("stripDraftSessionFacts keeps tasks for real sessions", () => {
  const resident = sessionArtifact();
  resident.session = { id: "session-a", name: "A" };
  resident.task = { request_id: "task-a", status: "progressing", context_compactions: [{ version: 1 }] };
  const kept = shape.stripDraftSessionFacts(resident);
  assert.equal(kept.task.context_compactions.length, 1, "非草稿会话的任务面必须原样保留");
  assert.equal(kept, resident, "无可清字段时返回原对象（不制造无意义拷贝）");
});

test("stripDraftSessionFacts recognizes drafts by either field", () => {
  const byStatus = sessionArtifact();
  byStatus.session = { id: "draft_2_1", name: "新会话", status: "draft" };
  byStatus.task = { request_id: "task-x", context_compactions: [{ version: 1 }] };
  assert.equal(shape.stripDraftSessionFacts(byStatus).task, undefined, "status=draft 同样认");

  const byFlag = sessionArtifact();
  byFlag.session = { id: "draft_3_1", name: "新会话", draft: true };
  byFlag.task = { request_id: "task-x", context_compactions: [{ version: 1 }] };
  assert.equal(shape.stripDraftSessionFacts(byFlag).task, undefined, "draft=true 同样认");

  assert.equal(shape.isDraftSession({}), false, "没有 session 段的载荷不算草稿");
  assert.equal(shape.isDraftSession(null), false);
});
