// snapshot-shape.js ── 快照分型的字段归属契约（G3）
//
// 与 application/model 的 SessionSnapshot/SessionRuntime/
// ProcessSnapshot/ProcessRuntime JSON 键一一对应（target-design §2.1/§2.3/
// §2.4；Go 侧形状契约见 application/core/session_snapshot_test.go）。
// 本模块是前端的只读类型表：桌面 Workbench 仍以联合 Snapshot 下发时，
// 消费方按本表区分会话字段与进程字段；会话粒度制品（capabilities 声明
// session_snapshot）与进程制品到达后，泄漏校验与归属提取全部走这里。
// 不持有任何业务状态：纯函数，供 reducer 与契约测试引用。

// SESSION_RUNTIME_KEYS 是 model.SessionRuntime 的 JSON 键：每个会话一份，
// 随会话快照传输（会话专属运行原件）。
export const SESSION_RUNTIME_KEYS = Object.freeze([
  "effort",
  "full_access",
  "tokens",
  "replan",
  "plan",
  "todo_items",
  "subagent_tree",
  "goal_skill_active",
  "active_skills",
  "work_table",
  "work_table_batches"
]);

// PROCESS_RUNTIME_KEYS 是 model.ProcessRuntime 的 JSON 键：进程单例原件
// （model/provider/account/plugin、能力清单与定时任务），不进入会话快照。
export const PROCESS_RUNTIME_KEYS = Object.freeze([
  "model",
  "provider",
  "account",
  "plugin",
  "visible_tools",
  "skills",
  "plugins",
  "accounts",
  "scheduled_tasks",
  "scheduled_commands"
]);

// SESSION_TOP_KEYS 是 SessionSnapshot 顶层 JSON 键（不含 protocol_version/
// revision/capabilities——两者形状都携带）。
export const SESSION_TOP_KEYS = Object.freeze([
  "session",
  "conversation",
  "chat",
  "task",
  "approvals",
  "history_offset",
  "total_messages",
  "has_more_history",
  "conversation_window",
  "read_files"
]);

// PROCESS_TOP_KEYS 是 ProcessSnapshot 顶层 JSON 键：会话目录/工作区/绑定。
export const PROCESS_TOP_KEYS = Object.freeze([
  "sessions",
  "workspaces",
  "session_workspaces",
  "current_workspace"
]);

export function isSessionRuntimeKey(key) {
  return SESSION_RUNTIME_KEYS.includes(key);
}

export function isProcessRuntimeKey(key) {
  return PROCESS_RUNTIME_KEYS.includes(key);
}

// splitRuntime 把一份（联合快照的）runtime 按归属拆成两个独立对象：
// { session, process }。归属表之外的未来键保留在 session 侧，绝不丢弃
// （未知键按保守口径归会话，等待模型层把新字段正式归类）。
export function splitRuntime(runtime = {}) {
  const session = {};
  const process = {};
  for (const [key, value] of Object.entries(runtime)) {
    if (isProcessRuntimeKey(key)) process[key] = value;
    else session[key] = value;
  }
  return { session, process };
}

// sessionRuntimeOf 提取会话专属运行原件（effort/plan/worktable/子代理树…）。
export function sessionRuntimeOf(runtime = {}) {
  return splitRuntime(runtime).session;
}

// processRuntimeOf 提取进程级运行原件（model/accounts/skills/可见工具…
// 定时任务）。桌面 Workbench 的联合 runtime 直接可拆；会话粒度制品不含
// 这些字段，返回空对象——进程上下文需由宿主从 Workbench/ProcessSnapshot
// 保留（见 processContextOf）。
export function processRuntimeOf(runtime = {}) {
  return splitRuntime(runtime).process;
}

// classifySnapshot 判定一份快照制品的形状：
//   "workbench" —— 联合桌面快照（conversation + sessions 都在；进程字段仍
//                  内联在 runtime，模型层分型前的现状传输形状）；
//   "session"   —— 会话粒度制品（有 conversation/session，无会话目录；
//                  capabilities.session_snapshot=true 时按严格形状校验）；
//   "process"   —— 进程粒度制品（有 sessions，无 conversation）；
//   "unknown"   —— 形状无法识别（拒绝解析）。
export function classifySnapshot(snapshot) {
  if (!snapshot || typeof snapshot !== "object") return "unknown";
  const hasConversation = Array.isArray(snapshot.conversation);
  const hasCatalog = Array.isArray(snapshot.sessions);
  if (hasCatalog && !hasConversation) return "process";
  if (hasConversation && hasCatalog) return "workbench";
  if (hasConversation) return "session";
  return "unknown";
}

// processContextOf 从 Workbench 联合快照（或 ProcessSnapshot）提取进程侧
// 上下文：目录/工作区/绑定 + 进程运行原件。会话粒度制品本身不含进程字段，
// 桌面在切换为 SessionSnapshot 展示时保留这份上下文，进程面板（账户/
// 插件/技能/定时任务/模型）不随会话载荷抖动。
export function processContextOf(snapshot = {}) {
  const context = { runtime: processRuntimeOf(snapshot.runtime) };
  for (const key of PROCESS_TOP_KEYS) {
    if (snapshot[key] !== undefined) context[key] = snapshot[key];
  }
  if (snapshot.capabilities !== undefined) context.capabilities = snapshot.capabilities;
  return context;
}

// assertTypedShape 校验会话/进程制品的字段归属（INV-G1 前端镜像）：
//   - 会话制品（capabilities.session_snapshot=true）不得携带会话目录/工作区
//     等进程顶层键，runtime 不得携带进程运行原件；
//   - 进程制品不得携带 conversation/chat/session 等会话字段，runtime 不得
//     携带 effort/plan/work_table 等会话运行原件；
//   - workbench 联合快照（分型前的传输形状）两种字段都允许，不做交叉校验。
// 违例抛错；通过时返回 classifySnapshot 的结果。
export function assertTypedShape(snapshot) {
  const kind = classifySnapshot(snapshot);
  if (kind === "unknown") {
    throw new Error("GUI snapshot 无法识别为 workbench/session/process 形状");
  }
  const runtime = snapshot.runtime || {};
  if (kind === "session" && snapshot.capabilities?.session_snapshot === true) {
    const leakedProcess = PROCESS_TOP_KEYS.filter(key => snapshot[key] !== undefined);
    const leakedRuntime = PROCESS_RUNTIME_KEYS.filter(key => runtime[key] !== undefined);
    if (leakedProcess.length || leakedRuntime.length) {
      throw new Error(`会话快照泄漏进程字段: ${[...leakedProcess, ...leakedRuntime].join(", ")}`);
    }
  }
  if (kind === "process") {
    const leakedSession = SESSION_TOP_KEYS.filter(key => snapshot[key] !== undefined);
    const leakedRuntime = SESSION_RUNTIME_KEYS.filter(key => runtime[key] !== undefined);
    if (leakedSession.length || leakedRuntime.length) {
      throw new Error(`进程快照泄漏会话字段: ${[...leakedSession, ...leakedRuntime].join(", ")}`);
    }
  }
  return kind;
}
