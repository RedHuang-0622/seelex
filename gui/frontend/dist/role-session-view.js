import { escapeHtml, icon } from "./components.js";

// ── 角色会话只读观察面（团队看板「当前会话」入口）──────────────
//
// 这一份是从 agent-team-view.js 切出来的**只读那一面**：Agent Team 面板本体已退场
// （2026-10-12 J-1a，用户裁决：团队手工召唤删除、`@` 让给文件指代），但「看这位员工
// 此刻那件事的会话」这条读面不属于面板——它的入口在**团队看板**（`team-board-view.js`
// 的在编行写钩子 `data-team-role-open`，`app.js` 的 `bindTeamBoardActions` 绑委托），
// 因此原样保留，只把口径从「Agent Team 面板成员行」改成「角色会话只读观察面」。
//
// 数据源：Bridge.AgentTeamRoleSnapshot（application/contract/dto.RoleSnapshot）——后端
// 保留该方法且不改名（它是这条读面的权威读数，与面板存亡无关）。
// 目的：EXEC（main）与 ADVISOR（tl）是两个独立会话；主对话只显示 EXEC 的可见消息，
// ADVISOR 自己的行在这里按角色身份单独列出，避免「两个 agent 都叫 AGENT」的歧义。
// 纯渲染，不写任何状态。

// roleDisplayName 把逻辑角色名映射成用户可读的 agent 身份：DS-A2A 里
// main 是 EXEC（执行会话 a），tl/techlead 是 ADVISOR（评审会话 b）。
// 两个 agent 的正文此前都渲染成同一个 AGENT/Seelex，用户无法区分。
export function roleDisplayName(roleName, roleKind = "") {
  const name = String(roleName || "").trim();
  const kind = String(roleKind || "").trim();
  if (name === "main" || kind === "main") return "EXEC";
  if (name === "tl" || kind === "techlead") return "ADVISOR";
  if (name === "user" || kind === "user") return "USER";
  return name || kind || "AGENT";
}

function shortID(value) {
  const text = String(value || "");
  return text.length > 12 ? `${text.slice(0, 12)}…` : text;
}

// DRAFT_KIND_LABEL 把草稿行的 kind 翻成用户可读的类别（后端 goal_team_recorder
// 的三条生产者：role_context / tl_directive / round_host，逃生收口是 goal_archive）。
// 未登记的 kind 原样显示——不猜。
const DRAFT_KIND_LABEL = {
  role_context: "本轮上下文",
  tl_directive: "本轮裁决",
  round_host: "本轮主持",
  goal_archive: "收口归档"
};

function normalizeRoleRow(item) {
  const row = item && typeof item === "object" ? item : {};
  return {
    seq: Number.isInteger(row.seq) ? row.seq : 0,
    kind: typeof row.kind === "string" ? row.kind : "",
    role: typeof row.role === "string" ? row.role : "",
    roleName: typeof row.role_name === "string" ? row.role_name : "",
    roleSessionID: typeof row.role_session_id === "string" ? row.role_session_id : "",
    content: typeof row.content === "string" ? row.content : "",
    reasoning: typeof row.reasoning_content === "string" ? row.reasoning_content : "",
    toolCalls: Array.isArray(row.tool_calls) ? row.tool_calls.filter(call => call && typeof call === "object") : []
  };
}

function isRenderableRoleRow(row) {
  return Boolean(row.content || row.reasoning || row.toolCalls.length);
}

function normalizeRoleRows(items) {
  if (!Array.isArray(items)) return [];
  return items.map(normalizeRoleRow).filter(isRenderableRoleRow);
}

// draftIDOf 读草稿的轮次 / 单元号：外层（sequencer 的排序凭据）优先，内层 event
// 兜底——两边都可能是 0（"还没分配"），0 不代表任何一个回合。
function draftIDOf(outer, inner) {
  if (Number.isInteger(outer) && outer > 0) return outer;
  return Number.isInteger(inner) && inner > 0 ? inner : 0;
}

// normalizeRoleDrafts 归一化**未同步草稿**（dto.RoleDraftRow）：外层是 sequencer
// 的排序/幂等凭据，内层 event 才是将来要 append 进 message 的行。
//
// 为什么保留 round_id / unit_seq / kind：草稿还没有 message seq（seq 由 sequencer
// 在同步时分配），所以它们的身份是"第几轮的第几个单元 + 类别"；把它们当普通行按
// seq 处理会让同一批草稿全部落到 seq 0 上互相覆盖。
function normalizeRoleDrafts(items) {
  if (!Array.isArray(items)) return [];
  return items.map(item => {
    const row = item && typeof item === "object" ? item : {};
    const event = row.event && typeof row.event === "object" ? row.event : {};
    return {
      ...normalizeRoleRow(event),
      roundID: draftIDOf(row.round_id, event.round_id),
      unitSeq: draftIDOf(row.unit_seq, event.unit_seq)
    };
  }).filter(isRenderableRoleRow);
}

export function normalizeRoleSession(snapshot) {
  const source = snapshot && typeof snapshot === "object" ? snapshot : {};
  const floor = source.floor && typeof source.floor === "object" ? source.floor : {};
  return {
    mainSessionID: typeof source.main_session_id === "string" ? source.main_session_id : "",
    roleName: typeof source.role_name === "string" ? source.role_name : "",
    roleSessionID: typeof source.role_session_id === "string" ? source.role_session_id : "",
    root: typeof source.root === "string" ? source.root : "",
    joinSeqID: Number.isFinite(source.join_seq_id) ? source.join_seq_id : 0,
    prefixCutSeq: Number.isFinite(source.prefix_cut_seq) ? source.prefix_cut_seq : 0,
    orderPolicy: typeof source.order_policy === "string" ? source.order_policy : "",
    orderRoles: Array.isArray(source.order_roles) ? source.order_roles.filter(name => typeof name === "string" && name) : [],
    floorRole: typeof floor.role_name === "string" ? floor.role_name : "",
    mainRows: normalizeRoleRows(source.main_rows),
    visibleMainRows: Number.isInteger(source.visible_main_rows) ? source.visible_main_rows : 0,
    outsidePrefixMainRows: Number.isInteger(source.outside_prefix_main_rows) ? source.outside_prefix_main_rows : 0,
    roleRows: normalizeRoleRows(source.role_rows),
    draftRows: normalizeRoleDrafts(source.draft_rows),
    unassignedRoleRows: Number.isInteger(source.unassigned_role_rows) ? source.unassigned_role_rows : 0,
    designWarnings: Array.isArray(source.design_warnings) ? source.design_warnings.filter(item => typeof item === "string" && item) : []
  };
}

// renderRoleSessionSwitcher 渲染「切员工」切换条：对话视图（员工会话）顶部的一排
// 员工 chip，点谁就把视图目标换成谁的角色会话——不用关掉视图再回列表点下一位。
// 只在拿到在职员工名单时渲染（旧宿主/单员工时不摆一条只有一个 chip 的条）。
export function renderRoleSessionSwitcher(members, currentRoleName) {
  const list = (Array.isArray(members) ? members : [])
    .filter(member => member && typeof member.roleName === "string" && member.roleName);
  if (list.length <= 1) return "";
  const current = String(currentRoleName || "");
  const chips = list.map(member => {
    const active = member.roleName === current;
    const display = roleDisplayName(member.roleName, member.roleKind);
    return `<button type="button" class="role-session-switch-chip${active ? " is-active" : ""}" data-role-session-switch="${escapeHtml(member.roleName)}" data-role-session-switch-sid="${escapeHtml(member.roleSessionID || "")}"${active ? ' aria-current="true" title="正在看这一位"' : ` title="切到 ${escapeHtml(display)} 的会话"`}>${escapeHtml(display)}</button>`;
  }).join("");
  return `<div class="role-session-switcher" role="tablist" aria-label="切换员工会话">${chips}</div>`;
}

export function renderRoleSessionDetail(snapshot, identity = null) {
  const view = normalizeRoleSession(snapshot);
  const roleName = String(identity?.roleName || view.roleName || "");
  const roleSessionID = String(identity?.roleSessionID ?? view.roleSessionID ?? "");
  const display = roleDisplayName(view.roleName);
  // 手动刷新键（E1）：权威读数（角色会话投影）是**拉取**面，刷新键是"我现在就要最新"的
  // 兜底。但"此刻在做什么"不靠它——那条走 teammate.tool.started/completed 的**推送**
  // 路（见 renderRoleLiveTools 与 app.js 的 applyTeammateToolActivity）：员工一动手，
  // 这一节就逐帧长出来，不必等这一轮跑完（2026-10-03 口径修正）。
  const toolbar = `<div class="role-session-toolbar">
      <span class="role-session-toolbar-hint muted">运行详情 · 实时（teammate.tool.*）+ 手动刷新</span>
      <button type="button" class="image-button" data-role-session-refresh="1" data-role-session-role="${escapeHtml(roleName)}" data-role-session-sid="${escapeHtml(roleSessionID)}" title="重新拉取这个员工会话的读数（权威记录是拉取面）" aria-label="刷新员工运行详情">${icon("refresh", 12)}</button>
    </div>`;
  // 「切员工」切换条（用例 5 的对话视图）：把视图目标从一个员工会话换成另一个。
  const switcher = renderRoleSessionSwitcher(identity?.members, roleName);
  const warnings = view.designWarnings.length
    ? `<div class="team-notice" role="status">${view.designWarnings.map(escapeHtml).join("；")}</div>`
    : "";
  const header = `<div class="role-session-ident">
      <span class="chip role-session-identity">${escapeHtml(display)}</span>
      <span class="role-session-name" title="role_name（逻辑角色名）">${escapeHtml(view.roleName || "—")}</span>
      <span class="role-session-sid muted" title="role_session_id">${escapeHtml(shortID(view.roleSessionID) || "会话未创建")}</span>
      ${view.floorRole ? `<span class="chip team-floor-chip">发言中 · ${escapeHtml(view.floorRole)}</span>` : ""}
    </div>
    <div class="role-session-meta muted">
      main ${escapeHtml(shortID(view.mainSessionID) || "—")} · join_seq ${view.joinSeqID} · policy ${escapeHtml(view.orderPolicy || "—")}${view.orderRoles.length ? ` · order ${escapeHtml(view.orderRoles.join(" → "))}` : ""}
    </div>`;
  const record = renderRoleRecordTable(snapshot);
  // 「正在做（实时）」——这一节的数据**不进快照**：它来自 teammate.tool.started/completed
  // 事件（见 app.js 的 applyTeammateToolActivity），是"此刻在做什么"的瞬态。放在记录表
  // 之前：先看它此刻在动什么，再看它的记录。
  const live = renderRoleLiveTools(identity?.liveTools);
  // 已发布的 message 行与未同步草稿**各自成区**：草稿是 sequencer 的 WAL，还没成为
  // 这个角色的 message；混在一起时用户看不出"哪一行还没同步"。
  const published = view.roleRows.length
    ? `<div class="role-session-rows">${view.roleRows.map(row => renderPublishedRoleRow(row, display)).join("")}</div>`
    : "";
  const drafts = renderRoleDraftBlock(view, display);
  if (!published && !drafts && !record && !live) {
    return `<div class="role-session-detail" data-role-session="${escapeHtml(view.roleName)}">${switcher}${toolbar}${warnings}${header}
      <div class="role-session-empty muted">该角色还没有独立会话行（未发言或尚未同步）。</div></div>`;
  }
  return `<div class="role-session-detail" data-role-session="${escapeHtml(view.roleName)}">${switcher}${toolbar}${warnings}${header}${live}${record}${published}${drafts}</div>`;
}

// ROLE_LIVE_STATUS_LABEL 把 teammate.tool.* 载荷的状态翻成面板上的短词。
// 未知取值**原样显示**（后端加新状态时，前端把它写死成 running 就是把事实说错）。
const ROLE_LIVE_STATUS_LABEL = { running: "进行中", success: "完成", error: "失败" };

// renderRoleLiveTools 渲染「正在做（实时）」一节：这位 teammate 此刻的工具活动。
//
// 数据来源是 `teammate.tool.started/completed` 事件的载荷（**不进快照**的瞬态，与
// subagent 详情里的 tool_events 同一形态）：一条一步、最新在下。它是"正在发生什么"，
// 不是权威记录——权威记录在下面的记录表与已发布/草稿行里（那两份来自角色会话投影，
// 这一份来自事件流）。没有活动 → 不渲染空壳。
export function renderRoleLiveTools(steps) {
  const list = (Array.isArray(steps) ? steps : []).filter(step => step && step.id);
  if (list.length === 0) return "";
  const rows = list.map(step => {
    const status = String(step.status || "").trim();
    const label = ROLE_LIVE_STATUS_LABEL[status] || status || "—";
    const detail = String(step.result || step.error || step.arguments || "").replace(/\s+/g, " ").trim();
    const shown = detail.length > 160 ? `${detail.slice(0, 159)}…` : detail;
    return `<li class="role-live-step is-${escapeHtml(status)}" data-live-id="${escapeHtml(String(step.id))}">
        <span class="chip role-live-status is-${escapeHtml(status)}">${escapeHtml(label)}</span>
        <code class="role-live-name">${escapeHtml(String(step.name || "?"))}</code>
        ${shown ? `<span class="role-live-detail" title="${escapeHtml(detail)}">${escapeHtml(shown)}</span>` : ""}
      </li>`;
  }).join("");
  return `<section class="team-block role-live-tools" data-role-live-tools="${list.length}">
      <div class="section-title sub-title"><span>正在做（实时）</span><span class="badge">${list.length}</span></div>
      <div class="role-session-meta muted">每一行都来自 ${escapeHtml("teammate.tool.*")} 事件（不进快照的瞬态）：这一轮还没跑完时，这里就是它此刻在做什么。</div>
      <ul class="role-live-steps">${rows}</ul>
    </section>`;
}

// renderPublishedRoleRow 是**已发布**的角色行（同步进 message 之后的行，有 seq）。
function renderPublishedRoleRow(row, display) {
  return `<article class="role-session-row">
      <div class="role-session-row-head"><strong>${escapeHtml(display)}</strong><span class="muted">seq ${row.seq}</span><span class="muted">${escapeHtml(row.role)}</span></div>
      ${row.reasoning ? `<div class="role-session-reasoning muted">${escapeHtml(row.reasoning)}</div>` : ""}
      ${row.content ? `<div class="role-session-text">${escapeHtml(row.content)}</div>` : ""}
      ${row.toolCalls.map(call => `<div class="role-session-tool muted">tool ${escapeHtml(call.name || "?")} ${escapeHtml(call.arguments || "")}</div>`).join("")}
    </article>`;
}

// renderRoleDraftBlock 渲染「未同步草稿」独立区：条数徽标 + 每行 is-draft 标记 +
// 待同步文案 + 轮次/单元/类别（草稿还没有 seq，不能拿 seq 冒充身份）。
//
// 生命周期口径：草稿在本轮**完成**时由 sequencer 同步成 message 行（SyncRoleDraft：
// 发布 head+floor 后删除 draft 文件），本轮**取消/结束**时同理收敛——前端只渲染后端
// 权威投影，不自己推演"同步后应该长什么样"；草稿区消失的唯一原因是后端
// snapshot.draft_rows 变空。
function renderRoleDraftBlock(view, display) {
  if (!view.draftRows.length) return "";
  const items = view.draftRows.map((row, index) => {
    const kind = DRAFT_KIND_LABEL[row.kind] || row.kind || "草稿";
    return `<article class="role-session-row is-draft" data-draft-index="${index + 1}" data-draft-kind="${escapeHtml(row.kind)}" data-draft-round="${row.roundID}" data-draft-unit="${row.unitSeq}" title="未同步草稿：本轮结束（完成或取消）同步后才写进 message">
      <div class="role-session-row-head"><strong>${escapeHtml(display)}</strong><span class="muted">${escapeHtml(draftRowMeta(row))}</span><span class="chip is-draft">待同步</span><span class="muted">${escapeHtml(kind)}</span><span class="muted">${escapeHtml(row.role)}</span></div>
      ${row.reasoning ? `<div class="role-session-reasoning muted">${escapeHtml(row.reasoning)}</div>` : ""}
      ${row.content ? `<div class="role-session-text">${escapeHtml(row.content)}</div>` : ""}
      ${row.toolCalls.map(call => `<div class="role-session-tool muted">tool ${escapeHtml(call.name || "?")} ${escapeHtml(call.arguments || "")}</div>`).join("")}
    </article>`;
  }).join("");
  return `<div class="team-block role-session-drafts" data-role-drafts="${view.draftRows.length}">
    <div class="section-title sub-title"><span>未同步草稿</span><span class="badge">${view.draftRows.length}</span></div>
    <div class="role-session-meta muted">本轮还没结束（或上一次同步没成功）：这些行还在 draft 里，同步成功后才出现在上面的 message 行中。</div>
    <div class="role-session-rows">${items}</div>
  </div>`;
}

// draftRowMeta 是草稿行的身份串：轮次 + 单元（后端 sequencer 的排序键）。
function draftRowMeta(row) {
  const parts = [];
  if (row.roundID > 0) parts.push(`round ${row.roundID}`);
  if (row.unitSeq > 0) parts.push(`unit ${row.unitSeq}`);
  return parts.length ? parts.join(" · ") : "未分配回合号";
}

// renderRoleRecordTable 用**竖排记录**表达该 teammate 自己那份 team work 记录：
// **一条回合一行**，行里两栏 = main 车道 / 它自己那条车道。
//
// 为什么竖排（2026-10-03 现场口径）：修前是横向 excle-grid——车道当行、回合号当列，
// 时间往右长。回合一多，窄弹窗里横向滚动才有内容，读起来是"一张表"而不是"一条过程"；
// 而这块记录要回答的恰恰是"这位在时间轴上依次经历了什么"。改成回合当行之后，时间
// 自上而下，回合序号就是行号，草稿行接在末尾（"草稿N"当行号——草稿还没有发布 seq，
// 不能冒充第 0 回合）。
//
//   main 车道 = 主会话自己的回合（整段）；
//   自身车道 = 它入伙之后的共享回合（seq > prefix_cut_seq，标成 is-shared）
//              + 它自己的行（标成 is-own）；
//              入伙之前（或已被压缩掉）的回合两栏都是占位 —（is-outside）：
//              **不冒充它记得的上下文**。
//
// 切点来自后端只读投影（prefix_cut_seq，判据与角色 wire 装配一致：join_seq_id，
// compact 后取更大的 applied_seq；main 复用主会话本身、切点恒为 0）。前端只渲染，
// 没有任何回写口——把渲染结果推回去会让后端真值变成前端派生物。
export function renderRoleRecordTable(snapshot) {
  const view = normalizeRoleSession(snapshot);
  const ownRows = view.roleRows;
  const draftRows = view.draftRows;
  if (!view.mainRows.length && !ownRows.length && !draftRows.length) return "";
  const display = roleDisplayName(view.roleName);
  const cut = view.prefixCutSeq;
  const mainBySeq = new Map(view.mainRows.map(row => [seqOf(row), row]));
  const ownBySeq = new Map(ownRows.map(row => [seqOf(row), row]));
  const seqs = [...new Set([...mainBySeq.keys(), ...ownBySeq.keys()])].sort((a, b) => a - b);
  const mainCell = (seq) => mainBySeq.has(seq)
    ? `<td class="role-record-cell is-main" data-seq="${seq}" title="主会话自己的回合">${escapeHtml(recordCellText(mainBySeq.get(seq)))}</td>`
    : `<td class="role-record-cell is-empty" data-seq="${seq}" title="这一回合主会话没有行">·</td>`;
  const ownCell = (seq) => {
    if (ownBySeq.has(seq)) {
      return `<td class="role-record-cell is-own" data-seq="${seq}" title="${escapeHtml(display)} 自己的回合">${escapeHtml(recordCellText(ownBySeq.get(seq)))}</td>`;
    }
    if (!mainBySeq.has(seq)) {
      return `<td class="role-record-cell is-empty" data-seq="${seq}" title="这一回合谁都没有行">·</td>`;
    }
    if (cut > 0 && seq <= cut) {
      return `<td class="role-record-cell is-outside" data-seq="${seq}" title="不在 ${escapeHtml(display)} 的前缀匹配区间">—</td>`;
    }
    return `<td class="role-record-cell is-shared" data-seq="${seq}" title="共享上下文（它能看到的 main 回合）">${escapeHtml(recordCellText(mainBySeq.get(seq)))}</td>`;
  };
  // 轮次行：一条回合一行（时间自上而下）；两条车道的归属各自成栏。
  const rounds = seqs.map(seq => `<tr class="role-record-round" data-seq="${seq}">
        <th class="role-record-seq" scope="row">${seq}</th>
        ${mainCell(seq)}
        ${ownCell(seq)}
      </tr>`).join("");
  // 草稿行接在末尾：行号是「草稿N」而不是 seq（seq 由 sequencer 在同步时分配）。
  const drafts = draftRows.map((row, index) => `<tr class="role-record-round is-draft" data-draft="${index + 1}">
        <th class="role-record-seq is-draft" scope="row" title="未同步草稿：同步后才分配 seq">草稿${index + 1}</th>
        <td class="role-record-cell is-empty" data-draft="${index + 1}" title="草稿只属于它自己的车道">·</td>
        <td class="role-record-cell is-own is-draft" data-draft="${index + 1}" title="未同步草稿：本轮结束（完成或取消）同步后才写进 message">${escapeHtml(recordCellText(row))}</td>
      </tr>`).join("");
  const legend = cut > 0
    ? `前缀匹配自 seq ${cut + 1} 起：更早的 ${view.outsidePrefixMainRows} 行不在它的记录里（占位 —）`
    : "该会话整段都在它的记录里";
  const draftNote = draftRows.length
    ? `；末尾 ${draftRows.length} 行「草稿N」是未同步草稿，同步后才成为 message 行（还没有 seq）`
    : "";
  return `<div class="role-record" data-record-cut="${cut}" data-record-outside="${view.outsidePrefixMainRows}" data-record-visible="${view.visibleMainRows}" data-record-own="${ownRows.length}" data-record-draft="${draftRows.length}">
      <div class="role-record-legend muted">${escapeHtml(legend + draftNote)}</div>
      <table class="role-record-table" data-role-record-table>
        <thead><tr class="role-record-head"><th class="role-record-seq" scope="col">回合</th><th class="role-record-lane-head" scope="col">main</th><th class="role-record-lane-head is-own" scope="col" title="${escapeHtml(display)}">${escapeHtml(view.roleName || display)}</th></tr></thead>
        <tbody>${rounds}${drafts}</tbody>
      </table>
    </div>`;
}

function seqOf(row) {
  const value = Number(row?.seq);
  return Number.isFinite(value) ? value : 0;
}

function recordCellText(row) {
  const value = String(row?.content || row?.reasoning || "").replace(/\s+/g, " ").trim();
  return value.length <= 60 ? value : `${value.slice(0, 60)}…`;
}
