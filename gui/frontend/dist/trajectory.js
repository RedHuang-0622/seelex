// 轨迹（Trajectory）——响应类型分类 + Network 风格轨迹渲染（纯函数，零依赖，可单测）。
//
// 数据源是 Snapshot.conversation（后端权威投影），本模块只做呈现层派生：
// 先按"响应类型"把会话消息分类，再把分类结果投影为类似浏览器 Network
// 面板的轨迹行（时间 / 类型 / 名称 / 状态 / 耗时 / 大小 + IN/OUT 详情）。
// 分类与渲染都是纯函数；DOM 交互（过滤、展开、result_ref 读回）在
// trajectory-view.js，本地 UI 状态不进入 Snapshot。

// 响应类型表：每条轨迹记录归属一个类型。顺序即过滤条展示顺序。
export const TRAJECTORY_KINDS = [
  { kind: "input",  label: "输入" },
  { kind: "llm",    label: "LLM" },
  { kind: "tool",   label: "工具" },
  { kind: "error",  label: "错误" },
  { kind: "system", label: "系统" },
  { kind: "notice", label: "通知" }
];

export function trajectoryKindLabel(kind) {
  return (TRAJECTORY_KINDS.find(entry => entry.kind === kind) || {}).label || String(kind || "?");
}

// 类型 → 状态着色 token（复用语义色：done/failed/running/info/idle）。
const KIND_STATUS = {
  input: "info",
  llm: "done",
  tool: "done",
  error: "failed",
  system: "info",
  notice: "idle"
};

export function trajectoryKindStatus(kind) {
  return KIND_STATUS[kind] || "idle";
}

const KIND_ICONS = {
  input: '<path d="M4 5h16v12H8l-4 4z"/>',
  llm: '<path d="M4 7h10M18 7h2M4 17h2M10 17h10"/><circle cx="16" cy="7" r="2"/><circle cx="8" cy="17" r="2"/>',
  tool: '<path d="m5 7 4 4-4 4M11 17h8"/>',
  error: '<circle cx="12" cy="12" r="9"/><path d="M12 7v6M12 17h.01"/>',
  system: '<rect x="3" y="5" width="18" height="14" rx="2"/><path d="m7 9 3 3-3 3M12 15h5"/>',
  notice: '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .34 1.88l.06.06-2.12 2.12-.06-.06a1.7 1.7 0 0 0-1.88-.34 1.7 1.7 0 0 0-1.04 1.56V20.3h-3v-.08A1.7 1.7 0 0 0 10.66 18.66a1.7 1.7 0 0 0-1.88.34l-.06.06-2.12-2.12.06-.06A1.7 1.7 0 0 0 7 15a1.7 1.7 0 0 0-1.56-1.04h-.08v-3h.08A1.7 1.7 0 0 0 7 9.92a1.7 1.7 0 0 0-.34-1.88L6.6 7.98l2.12-2.12.06.06a1.7 1.7 0 0 0 1.88.34A1.7 1.7 0 0 0 11.7 4.7v-.08h3v.08a1.7 1.7 0 0 0 1.04 1.56 1.7 1.7 0 0 0 1.88-.34l.06-.06 2.12 2.12-.06.06a1.7 1.7 0 0 0-.34 1.88 1.7 1.7 0 0 0 1.56 1.04h.08v3h-.08A1.7 1.7 0 0 0 19.4 15Z"/>'
};

export function trajectoryKindIcon(kind, size = 13) {
  const paths = KIND_ICONS[kind] || KIND_ICONS.notice;
  return `<svg class="icon" width="${size}" height="${size}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths}</svg>`;
}

// buildTrajectory 把 conversation 消息投影为轨迹记录数组（按消息顺序 =
// 时间顺序）。配对规则与 components.buildConversationItems 保持一致：
// role="tool" 消息记录请求（IN），后续 role="tool_result" 按 tool.id 命中
// 同一记录（name 回退），把响应（OUT）/状态/耗时/引用合并进去。
//
// 响应类型分类（先分类，再轨迹）：
//   role=user       → input  用户输入（请求发起）
//   role=assistant  → llm    模型响应（空占位跳过）
//   role=tool       → tool   工具调用（请求）
//   role=tool_result→ tool   工具响应（合并到配对记录）
//   role=error      → error  错误响应
//   role=system     → system 系统消息（SYSTEM）
//   其它/未知       → notice 兜底
export function buildTrajectory(messages = []) {
  const records = [];
  // 配对键 = 框架 tool-call id：tool.started 与 tool_result 携带同一个 id（恢复
  // 历史同样取 toolCall.ID）。按名字回退会把同名并发工具错配成一行，属于前端
  // 自造的业务判断，不再实现。
  const pendingByID = new Map();

  const push = record => {
    records.push(record);
    if (record.kind === "tool" && record.toolID) pendingByID.set(record.toolID, record);
    return record;
  };

  for (const [index, message] of messages.entries()) {
    const role = message.role || "assistant";
    const createdAt = message.created_at || "";
    if (!message.tool) {
      // 显式类别优先：历史记录条目携带 kind 时不再依赖 role 启发式。
      if (message.kind) {
        if (message.kind === "user_input") {
          push({ kind: "input", key: `message:${message.id || index}`, name: "输入", output: message.content || "", status: "info", startedAt: createdAt, duration: 0 });
        } else if (message.kind === "llm") {
          if (!message.content) continue;
          push({ kind: "llm", key: `message:${message.id || index}`, name: "LLM", output: message.content, reasoning: message.reasoning_content || "", status: "success", startedAt: createdAt, duration: 0 });
        } else if (message.kind === "error") {
          push({ kind: "error", key: `message:${message.id || index}`, name: "错误", output: message.content || "", status: "error", startedAt: createdAt, duration: 0 });
        } else if (message.kind === "system") {
          push({ kind: "system", key: `message:${message.id || index}`, name: "SYSTEM", output: message.content || "", status: "info", startedAt: createdAt, duration: 0 });
        } else {
          push({ kind: "notice", key: `message:${message.id || index}`, name: "通知", output: message.content || "", status: "info", startedAt: createdAt, duration: 0 });
        }
        continue;
      }
      if (role === "user") {
        push({ kind: "input", key: `message:${message.id || index}`, name: "输入", output: message.content || "", status: "info", startedAt: createdAt, duration: 0 });
      } else if (role === "assistant") {
        // 空 assistant 是工具回合后的占位消息，对轨迹无信息量，跳过。
        if (!message.content) continue;
        push({ kind: "llm", key: `message:${message.id || index}`, name: "LLM", output: message.content, reasoning: message.reasoning_content || "", status: "success", startedAt: createdAt, duration: 0 });
      } else if (role === "error") {
        push({ kind: "error", key: `message:${message.id || index}`, name: "错误", output: message.content || "", status: "error", startedAt: createdAt, duration: 0 });
      } else if (role === "system") {
        push({ kind: "system", key: `message:${message.id || index}`, name: "SYSTEM", output: message.content || "", status: "info", startedAt: createdAt, duration: 0 });
      } else {
        push({ kind: "notice", key: `message:${message.id || index}`, name: "通知", output: message.content || "", status: "info", startedAt: createdAt, duration: 0 });
      }
      continue;
    }

    const tool = message.tool;
    const isOutput = role === "tool_result";
    if (isOutput) {
      const target = tool.id ? pendingByID.get(tool.id) : null;
      if (target) {
        target.output = tool.error || tool.result || message.content || "";
        target.status = tool.error ? "error" : (tool.status === "error" || tool.status === "failed" ? "error" : "success");
        target.duration = tool.duration || target.duration;
        target.resultRef = tool.result_ref || "";
        target.truncated = Boolean(tool.truncated);
        target.totalChars = Number(tool.total_chars) || 0;
        continue;
      }
    }

    push({
      kind: "tool",
      key: `tool:${tool.id || message.id || index}`,
      toolID: tool.id || "",
      toolName: tool.name || "tool",
      name: tool.name || "tool",
      input: tool.arguments || "",
      output: tool.error || tool.result || (isOutput ? message.content || "" : ""),
      // 工具步骤的思考挂在发起它的那条消息上（持久化形状：assistant/tool_call
      // 行带 reasoning_content）。不带上它，轨迹里就只剩工具痕迹、看不到
      // "模型这一步在想什么"。
      reasoning: message.reasoning_content || "",
      status: tool.error ? "error"
        : (tool.status === "running" || tool.status === "pending" ? "running" : "success"),
      duration: tool.duration || 0,
      startedAt: createdAt,
      resultRef: tool.result_ref || "",
      truncated: Boolean(tool.truncated),
      totalChars: Number(tool.total_chars) || 0
    });
  }
  return records;
}

// renderTrajectoryWindowInfo 声明轨迹的窗口边界：轨迹由可见会话窗口派生（长会话
// 按 window 分页），所以"早期内容不在轨迹里"是分页结果、不是数据丢失。把
// 已加载/总量与"加载更早"入口摆在最上面，用户才知道自己看到的是哪一段。
export function renderTrajectoryWindowInfo(info = {}) {
  const messages = Math.max(Number(info.messages) || 0, 0);
  const total = Math.max(Number(info.total) || 0, 0);
  const hasMore = Boolean(info.hasMore);
  const scope = total > 0 && total !== messages
    ? `已加载窗口 <strong>${messages}</strong> / 会话共 <strong>${total}</strong> 条消息`
    : `已加载 <strong>${messages}</strong> 条消息`;
  const hint = hasMore
    ? "更早的回合尚未加载——轨迹与上下文轴只覆盖已加载的窗口"
    : "";
  const button = hasMore
    ? '<button type="button" class="text-button" data-trajectory-load-earlier>加载更早</button>'
    : "";
  return `<span class="trajectory-window-scope">${scope}</span>${hint ? `<span class="trajectory-window-hint">${hint}</span>` : ""}${button}`;
}

// filterTrajectory 按类型过滤（"all" = 不过滤）。Network 面板的过滤语义。
export function filterTrajectory(records, kind) {
  if (!kind || kind === "all") return records;
  return records.filter(record => record.kind === kind);
}

// trajectoryStats 统计摘要（Network 面板的请求状态汇总）。
export function trajectoryStats(records = []) {
  const stats = { total: records.length, success: 0, error: 0, running: 0, info: 0, byKind: {} };
  for (const record of records) {
    stats.byKind[record.kind] = (stats.byKind[record.kind] || 0) + 1;
    if (record.status === "error") stats.error += 1;
    else if (record.status === "running" || record.status === "pending") stats.running += 1;
    else if (record.status === "success") stats.success += 1;
    else stats.info += 1;
  }
  return stats;
}

// 过滤条（全部/输入/LLM/工具/错误/通知 + 计数徽标）。
export function renderTrajectoryFilters(records, active) {
  const counts = trajectoryStats(records).byKind;
  const buttons = [
    { kind: "all", label: "全部", count: records.length }
  ].concat(TRAJECTORY_KINDS.map(entry => ({ kind: entry.kind, label: entry.label, count: counts[entry.kind] || 0 })));
  return `<div class="trajectory-filters" role="tablist" aria-label="按响应类型过滤轨迹">
    ${buttons.map(button => `
      <button type="button" class="trajectory-filter-btn${button.kind === active ? " is-active" : ""}" data-trajectory-filter="${button.kind}" role="tab" aria-selected="${button.kind === active ? "true" : "false"}">
        <span class="trajectory-filter-label">${escapeHtml(button.label)}</span><span class="trajectory-filter-count">${button.count}</span>
      </button>`).join("")}
  </div>`;
}

// 摘要条（Network 面板顶部的统计行）。
export function renderTrajectorySummary(stats) {
  const statuses = [
    stats.success ? `<span class="is-success">成功 ${stats.success}</span>` : "",
    stats.error ? `<span class="is-failed">失败 ${stats.error}</span>` : "",
    stats.running ? `<span class="is-running">运行 ${stats.running}</span>` : ""
  ].filter(Boolean).join(" · ");
  return `<div class="trajectory-summary">共 ${stats.total} 条${statuses ? ` · ${statuses}` : ""}</div>`;
}

// ── 上下文轴：多线谱的轨定义、入轨规则、位置语义与分页（重建版）───────────
//
// 数据源：records 来自 buildTrajectory(Snapshot.conversation)；prefixLayers 来自
// Bridge.PromptLayers（会话级当前栈，不进 Snapshot）；compactions 来自
// Snapshot.Task.ContextCompactions（后端每次压缩发布的公开元数据，随全量刷新
// 带回）。轴只消费公开字段，不携带 checkpoint 正文 / 工具结果 / 原始对话。
//
// 【规则 1】轨的定义与固定顺序（常量，不随数据重排）
//   ① 前缀注入（元数据轨，最上）：会话级常量——每次请求都先于对话注入的
//      system 前缀层；横跨整轴，段宽 = 层文本占比。
//   ② 输入：人在这一轮发出的请求（轮次起点）。
//   ③ LLM：模型对当前上下文给出的响应（决策）。
//   ④ 工具：模型发起的动作（请求 + 响应合并为一块）。
//   ⑤ 错误：失败响应（模型/工具/会话级错误）。
//   ⑥ 系统：框架产生的会话系统消息。
//   ⑦ 通知：无法归入以上类型的兜底消息。
//   ⑧ 压缩（元数据轨，最下）：对会话的改写事件（窗口外旧轮次折叠为摘要）。
//   为什么是这个顺序：中间六条按"一轮上下文的因果链"排——人先说（输入）→
//   模型想（LLM）→ 模型做（工具）→ 做坏了单列（错误）→ 框架自己说的话
//   （系统/通知）兜底；因果在上、兜底在下。两条元数据轨不参与记录序号坐标
//   （上面那条是"每轮都发生"的常量带，下面那条是"已经发生过"的事件刻度），
//   因此摆在记录轨外侧，读的时候不会与记录块混在一起。
//   空轨保留占位并画虚线（is-empty）："这一段没有该类块"是看得见的结论，
//   不会因为轨消失而被误读成顺序跳变。
//
// 【规则 2】块的入轨规则
//   入轨只由 record.kind（响应类型，见 buildTrajectory 的分类表）决定：
//   input/llm/tool/error/system/notice 各归一条轨，未知类型兜底进「通知」轨。
//   轮次（turn）与阶段不参与分轨——同一轮的输入、LLM、工具块各归各轨；轮次只
//   体现在序号顺序与块 tooltip（第 N 轮）上。
//
// 【规则 3】位置与宽度的唯一语义 = 序号槽位（顺序），不是体量
//   一个块占一槽：x = 页内序号 × 槽宽，宽 = 槽宽 = 100 / 本页容量。
//   "它为什么在这个位置"一句话：它是当前页里的第 k 条记录。
//   体量（字符数/耗时）不进入几何，只在块 tooltip、轨迹行大小列与详情里出现，
//   所以同一块绝不会出现两套位置语义。选顺序而不是体量的原因：① 体量随流式
//   输出持续增长，会把别处的块推着漂移；② 分轨视图里"第 k 条"跨轨可比，而
//   体量占比只有把别轨的块一起算进来才成立；③ 体量已有大小列承载。
//   同一轨内块的序号互不相同 → 槽位天然不重叠；跨页的块不渲染（页边界）。
//   槽宽 ≥ AXIS_WIDE_MIN_SLOT_PERCENT（6%）时块直接显示标签（is-wide），否则
//   只在悬停时显示——一条轨全是无字色条时读不出内容。
//
// 【规则 4】页与坐标系
//   页 = 一段连续的记录序号区间（固定条数，档位见 AXIS_PAGE_SIZE_STEPS），页号
//   从 0 起、按时间正序（第 1 页 = 最早的一段），一屏只画一页的块。
//   本页容量 = min(页大小, 总条数)：会话短到一页放得下时整段铺满横轴（块更宽、
//   更好读），放不下时槽宽恒为 1/页大小；末页不满就在右侧留白——留白是"本页
//   没满"的诚实表达，不靠拉伸现存块伪装成满页。
//
// 【规则 5】元数据轨与记录轨的对齐规则
//   压缩刻度与记录轨共用同一套"记录序号"坐标：刻度落在锚定记录槽位的右边界；
//   锚点在本页之前/之后时刻度钳到页边界（0 / 100）并标注"锚点在第 x 页"，点击
//   跳页（不静默位移）；锚点早于已加载窗口时（anchored=false）钳到轴起点并注明。
//   前缀注入轨不参与序号坐标（它是每轮都发生的常量带），因此页切换不改变它。

// AXIS_LANES 逐轨语义表：直接由 TRAJECTORY_KINDS 派生（顺序唯一事实源，不会
// 漂移），why 是"这一轨是什么"的一句话解释（用于轨标签 title 与文档/测试）。
const LANE_WHY = {
  input: "轮次起点：人在这一轮发出的请求",
  llm: "模型决策：对当前上下文给出的响应",
  tool: "模型动作：工具调用与其响应合并为一块",
  error: "失败响应：模型/工具/会话级错误",
  system: "框架消息：会话自身的系统提示与状态",
  notice: "兜底通知：无法归入以上类型的历史/内部消息"
};

export const AXIS_LANES = TRAJECTORY_KINDS.map(entry => ({
  kind: entry.kind,
  label: entry.label,
  why: LANE_WHY[entry.kind] || ""
}));

// ── 上下文轴分页（纯函数，无 DOM）────────────────────────────────────────
// 页大小档位：Shift+滚轮按档步进（到边界即停，给出可见提示）。
export const AXIS_PAGE_SIZE_STEPS = [8, 16, 32, 64, 128];
// 默认 16 条/页：槽宽 6.25% ≥ AXIS_WIDE_MIN_SLOT_PERCENT → 块默认显示标签。
export const AXIS_PAGE_SIZE_DEFAULT = 16;
// 槽宽达到该占比时块直接显示标签（其余只在悬停时显示）。
export const AXIS_WIDE_MIN_SLOT_PERCENT = 6;
// 滚轮累计阈值：一次滚轮手势会连发很多小 deltaY，累计到阈值才翻一页。
export const AXIS_WHEEL_THRESHOLD = 40;

// normalizeAxisPageSize 把任意输入收敛到档位表内的合法页大小（非法回落默认档）。
export function normalizeAxisPageSize(value) {
  const size = Number(value);
  return AXIS_PAGE_SIZE_STEPS.includes(size) ? size : AXIS_PAGE_SIZE_DEFAULT;
}

// stepAxisPageSize 在档位表上步进一档（direction > 0 = 页变大，否则变小）。
// 返回 { size, previous, changed, atMin, atMax }：changed=false 表示已到档位边界，
// 调用方据此给出"已是最小/最大"的可见反馈而不是静默忽略。
export function stepAxisPageSize(current, direction) {
  const size = normalizeAxisPageSize(current);
  const index = AXIS_PAGE_SIZE_STEPS.indexOf(size);
  const last = AXIS_PAGE_SIZE_STEPS.length - 1;
  const nextIndex = Math.min(Math.max(index + (Number(direction) > 0 ? 1 : -1), 0), last);
  return {
    size: AXIS_PAGE_SIZE_STEPS[nextIndex],
    previous: size,
    changed: nextIndex !== index,
    atMin: nextIndex === 0,
    atMax: nextIndex === last
  };
}

// axisPageWindow 计算分页窗口（纯函数）：页号钳位、本页容量、本页序号区间。
// 容量规则见【规则 4】：capacity = min(页大小, 总条数)（总条数 0 时取页大小，
// 只为了让空数据也有一个非零槽宽，不产生除零）。
export function axisPageWindow(total, pageSize, page = 0) {
  const count = Math.max(Number(total) || 0, 0);
  const size = normalizeAxisPageSize(pageSize);
  const pageCount = Math.max(Math.ceil(count / size), 1);
  const current = Math.min(Math.max(Math.trunc(Number(page)) || 0, 0), pageCount - 1);
  const capacity = count > 0 ? Math.min(size, count) : size;
  const start = current * capacity;
  return {
    total: count,
    pageSize: size,
    pageCount,
    page: current,
    capacity,
    start,
    end: Math.min(start + capacity, count),
    slot: 100 / capacity
  };
}

// axisPageForIndex 记录序号 → 页号（刻度跳页用；越界钳位）。
export function axisPageForIndex(index, total, pageSize) {
  const window = axisPageWindow(total, pageSize, 0);
  const target = Math.max(Math.trunc(Number(index)) || 0, 0);
  return axisPageWindow(total, window.pageSize, Math.floor(target / window.capacity)).page;
}

// resolveAxisPage 把"期望页号 + 内容锚点 + 尾页跟随"解析成最终页窗口：
//   - anchorKey 记录还在 → 跟随它（加载更早内容会整体前移，视图不能跟着跳位）；
//   - tail=true → 停在最后一页（新记录到达时继续跟随最新一段）；
//   - 否则按期望页号钳位（页数变小时不会停在空白页）。
export function resolveAxisPage(records = [], options = {}) {
  const list = Array.isArray(records) ? records : [];
  const base = axisPageWindow(list.length, options.pageSize, options.page);
  if (options.anchorKey) {
    const index = list.findIndex(record => record && record.key === options.anchorKey);
    if (index >= 0) return axisPageWindow(list.length, base.pageSize, Math.floor(index / base.capacity));
  }
  if (options.tail) return axisPageWindow(list.length, base.pageSize, base.pageCount - 1);
  return base;
}

// axisWheelStep 把滚轮增量聚合成一次翻页：同向累加、反向清零，累计到阈值才
// 返回 step（+1 = 下一页/更新，-1 = 上一页/更早）。返回累计值供调用方保存。
export function axisWheelStep(accumulated, deltaY, threshold = AXIS_WHEEL_THRESHOLD) {
  const delta = Number(deltaY) || 0;
  if (!delta) return { accumulated: 0, step: 0 };
  const same = accumulated === 0 || Math.sign(accumulated) === Math.sign(delta);
  const next = same ? accumulated + delta : delta;
  if (Math.abs(next) < threshold) return { accumulated: next, step: 0 };
  return { accumulated: 0, step: Math.sign(next) };
}

// axisBlocks 计算当前页的块落点（纯函数）：轨 = 响应类型，位置 = 页内序号槽位
// （见【规则 3】）。turn 是块所在的轮次（第 N 轮，第一个输入块之前记 0 = 开场），
// 只用于 tooltip，不参与几何。
export function axisBlocks(records = [], view = {}) {
  const list = Array.isArray(records) ? records : [];
  const window = axisPageWindow(list.length, view.pageSize, view.page);
  const lanes = AXIS_LANES.map(lane => ({ kind: lane.kind, label: lane.label, why: lane.why, blocks: [], empty: true }));
  const byKind = new Map(lanes.map(lane => [lane.kind, lane]));
  let turn = 0;
  for (const [index, record] of list.entries()) {
    if (record && record.kind === "input") turn += 1;
    if (index < window.start || index >= window.end) continue;
    const lane = byKind.get(record?.kind) || byKind.get("notice");
    lane.blocks.push({ record, key: record?.key || "", index, turn, x: (index - window.start) * window.slot, width: window.slot });
  }
  for (const lane of lanes) lane.empty = lane.blocks.length === 0;
  return { window, lanes };
}

// promptKindLabel 层种类中文标签（identity/base/effort/instructions/skill）。
export function promptKindLabel(kind) {
  switch (kind) {
  case "identity": return "身份";
  case "base": return "基础/系统提示";
  case "effort": return "力度";
  case "instructions": return "指令";
  case "skill": return "技能";
  default: return kind || "层";
  }
}

function promptKindOrder(kind) {
  switch (kind) {
  case "identity": return 0;
  case "base": return 1;
  case "effort": return 2;
  case "instructions": return 3;
  case "skill": return 4;
  default: return 5;
  }
}

// prefixLayerSegments 把当前会话的 prompt 前缀层投影为轴「前缀注入」轨的段：
// 按装配顺序排列，段宽=层文本占比、横跨整条轴。语义：system 前缀在每一轮
// 请求都先于对话注入且字节稳定，因此它在对话坐标上是一整条常量带——轴只
// 表达它的组成占比，不假装它有逐消息坐标。可 trace 粒度 = 会话级当前层
// （PromptStack 不持久化每请求的层增量历史，这是当前实现的边界）。
export function prefixLayerSegments(layers = []) {
  const list = (Array.isArray(layers) ? layers : []).filter(layer => layer && typeof layer === "object");
  const ordered = [...list].sort((a, b) => promptKindOrder(a.kind) - promptKindOrder(b.kind));
  const weights = ordered.map(layer => Math.max(String(layer.text || "").length, 1));
  const total = weights.reduce((sum, weight) => sum + weight, 0) || 1;
  let cursor = 0;
  return ordered.map((layer, index) => {
    const x = (cursor / total) * 100;
    const width = (weights[index] / total) * 100;
    cursor += weights[index];
    const kind = String(layer.kind || "layer");
    return {
      key: `prefix:${kind}:${index}`,
      kind,
      name: layer.name ? String(layer.name) : "",
      label: layer.name ? String(layer.name) : promptKindLabel(kind),
      text: layer.text != null ? String(layer.text) : "",
      x,
      width,
      chars: String(layer.text || "").length
    };
  });
}

function compactionReasonLabel(reason) {
  switch (reason) {
  case "context_budget": return "上下文预算达峰";
  case "large_tool_output": return "超大工具输出";
  default: return "上下文压缩";
  }
}

function formatNumber(value) {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(Number(value) || 0);
}

function recordTime(record) {
  if (!record || !record.startedAt) return NaN;
  const time = new Date(record.startedAt);
  return Number.isNaN(time.getTime()) ? NaN : time.getTime();
}

// compactionAnchorIndex 定位"压缩发生时会话已推进到的最后一条记录"：用最后
// 一条 startedAt <= compacted_at 的记录锚定（压缩发生在该记录之后）。找不到
// （压缩点早于已加载窗口）返回 -1，调用方把刻度钳到轴起点并注明。
function compactionAnchorIndex(records, compactedAt) {
  const target = compactedAt ? new Date(compactedAt).getTime() : NaN;
  if (Number.isNaN(target)) return records.length - 1;
  let anchor = -1;
  for (let index = 0; index < records.length; index++) {
    const time = recordTime(records[index]);
    if (Number.isNaN(time)) continue;
    if (time <= target) anchor = index;
  }
  return anchor;
}

// compactionMarks 把 Snapshot.Task.ContextCompactions 投影为轴「压缩」轨刻度。
// 只含公开元数据（version/reason/messages_before/estimated_tokens/compacted_at）。
//
// 对齐规则（见【规则 5】）：刻度与记录轨共用"记录序号"坐标——锚定记录是"压缩
// 发生时会话已推进到的最后一条记录"（compactionAnchorIndex），刻度落在该槽位的
// 右边界，即 x = (anchorIndex + 1 - 本页起点) × 槽宽。
// 钳位：锚点在本页之前 → x=0（offPage="before"）；在本页之后 → x=100
// （offPage="after"）；两者都带 anchorPage，点击可跳页。锚点早于已加载窗口
// （anchor < 0）→ anchored=false、x=0、offPage="unknown"。
export function compactionMarks(records = [], compactions = [], view = {}) {
  const list = (Array.isArray(compactions) ? compactions : []).filter(c => c && typeof c === "object");
  const rows = Array.isArray(records) ? records : [];
  const window = axisPageWindow(rows.length, view.pageSize, view.page);
  // 同一锚点上的多个刻度会落在同一个 x（同一件事的重复记录、或都被钳到页边界）：
  // 给它们一个 0,1,2… 的错位序号，渲染时朝轨道内侧横向错开，保证每个刻度都能点。
  const stacks = new Map();
  return list.map((compaction, index) => {
    const anchor = compactionAnchorIndex(rows, compaction.compacted_at);
    const aligned = anchor >= 0 ? (anchor + 1 - window.start) * window.slot : null;
    let offPage = "";
    if (anchor < 0) offPage = "unknown";
    else if (anchor < window.start) offPage = "before";
    else if (anchor >= window.end) offPage = "after";
    const stackKey = anchor >= 0 ? `a${anchor}` : "unknown";
    const stack = stacks.get(stackKey) || 0;
    stacks.set(stackKey, stack + 1);
    const x = aligned === null ? 0 : Math.min(Math.max(aligned, 0), 100);
    return {
      key: `compact:${index}`,
      version: Number(compaction.version) || index + 1,
      reason: String(compaction.reason || ""),
      reasonLabel: compactionReasonLabel(compaction.reason),
      messagesBefore: Number(compaction.messages_before) || 0,
      tokens: Number(compaction.estimated_tokens) || 0,
      timeLabel: compaction.compacted_at ? formatTime(compaction.compacted_at) : "—",
      x,
      stack,
      anchored: anchor >= 0,
      anchorIndex: anchor,
      anchorPage: anchor >= 0 ? axisPageForIndex(anchor, rows.length, window.pageSize) : 0,
      offPage,
      page: window.page
    };
  });
}

// renderAxisDetail 渲染被点中的元数据块详情（全部 escape，无未受控注入）。
// selection = { type: "prefix", layer } 或 { type: "compression", mark }。
export function renderAxisDetail(selection = {}) {
  if (!selection || typeof selection !== "object") return "";
  if (selection.type === "prefix" && selection.layer) return renderPrefixDetail(selection.layer);
  if (selection.type === "compression" && selection.mark) return renderCompressionDetail(selection.mark);
  return "";
}

function renderPrefixDetail(layer) {
  const kind = String(layer.kind || "layer");
  const name = layer.name ? escapeHtml(String(layer.name)) : "";
  return `<section class="axis-detail is-prefix" data-axis-detail>
    <header>
      <strong>前缀注入层 · ${escapeHtml(promptKindLabel(kind))}</strong>
      <button class="axis-detail-close" type="button" data-axis-detail-close title="收起详情">收起</button>
    </header>
    <div class="axis-detail-meta">
      <span>${escapeHtml(promptKindLabel(kind))}</span>
      ${name ? `<span>${name}</span>` : ""}
      <span>${layer.chars} chars</span>
      <span>每请求前置（system 前缀）</span>
    </div>
    <pre class="axis-detail-text">${escapeHtml(layer.text) || '<span class="muted">（空层）</span>'}</pre>
  </section>`;
}

function renderCompressionDetail(mark) {
  const meta = [
    mark.timeLabel && mark.timeLabel !== "—" ? `时间 ${mark.timeLabel}` : "",
    mark.messagesBefore > 0 ? `${formatNumber(mark.messagesBefore)} 条消息` : "",
    mark.tokens > 0 ? `约 ${formatNumber(mark.tokens)} tokens` : ""
  ].filter(Boolean).join(" · ");
  const where = mark.anchored
    ? (mark.offPage
      ? `锚点在第 ${mark.anchorPage + 1} 页：本页刻度是钳位标记（点刻度跳页）`
      : "刻度=压缩发生时会话推进到的位置（锚定记录槽位右边界）")
    : "刻度钳在轴起点：压缩点早于当前已加载的对话窗口";
  return `<section class="axis-detail is-compression" data-axis-detail>
    <header>
      <strong>上下文压缩 #${escapeHtml(String(mark.version))}</strong>
      <button class="axis-detail-close" type="button" data-axis-detail-close title="收起详情">收起</button>
    </header>
    <div class="axis-detail-meta">
      <span>${escapeHtml(mark.reasonLabel)}</span>
      ${meta ? `<span>${escapeHtml(meta)}</span>` : ""}
      <span>${escapeHtml(where)}</span>
    </div>
    <p>该时刻窗口外的旧轮次被折叠为栈顶压缩摘要，随后装配保留一个有界的新鲜
       窗口继续执行。对话原文仍完整保留在时间线上（呈现层不丢消息）；折叠原文
       可经 read_compressed_turn(segment_id) / search_history 读回。</p>
  </section>`;
}

function normalizeAxisExtras(extras) {
  const options = extras && typeof extras === "object" ? extras : {};
  return {
    prefixLayers: Array.isArray(options.prefixLayers) ? options.prefixLayers : [],
    compactions: Array.isArray(options.compactions) ? options.compactions : [],
    // 分页可选：不提供时按默认页大小渲染第 1 页（既有调用方无需改动）。
    page: Number(options.page) || 0,
    pageSize: normalizeAxisPageSize(options.pageSize),
    // 瞬时反馈（页大小变更、更早内容未加载…）由视图提供，随渲染保留。
    notice: options.notice ? String(options.notice) : "",
    // hasMore：可见窗口之前是否还有未加载的回合（app.js 的 window 边界的同源
    // 信号）；用于在最早一页上明确写出"更早的回合尚未加载"。
    hasMore: Boolean(options.hasMore)
  };
}

// renderPrefixLane 渲染顶部「前缀注入」轨：每次请求前置的 system 前缀层，横跨
// 整轴（段宽 = 层文本占比）。该轨不参与记录序号坐标（【规则 5】），页切换不
// 改变它——它表达的是"每一轮请求都发生"。点击段开轴下方详情。
function renderPrefixLane(segments) {
  const bar = segments.map((segment, index) => {
    const title = `${segment.label} · ${segment.chars} chars · ${promptKindLabel(segment.kind)} · 每轮请求都前置（不随页切换变化） · 点击查看注入文本`;
    return `<button type="button" class="axis-segment is-prefix is-${escapeHtml(segment.kind || "layer")}" style="--x:${segment.x.toFixed(3)}%;--w:${segment.width.toFixed(3)}%" data-prefix-layer="${index}" title="${escapeHtml(title)}" aria-label="${escapeHtml(segment.label)}"><span>${escapeHtml(segment.label)}</span></button>`;
  }).join("");
  return `<div class="context-axis-lane is-prefix" aria-label="前缀注入（每次请求前置的 system 前缀层；段宽=层文本占比，横跨整轴，不随页切换变化）">
    <span class="axis-lane-label" title="每次请求都会在对话之前注入的 system 前缀层（identity/base/effort/instructions/skill）；横轴=各层文本占比，不参与记录序号坐标"><span>前缀注入</span><span class="axis-lane-count">${segments.length}</span></span>
    <div class="axis-lane-bar is-meta">${bar}</div>
  </div>`;
}

// compressionMarkWhere 一句话解释刻度的位置来源（含钳位情况）。
function compressionMarkWhere(mark) {
  if (!mark.anchored) return "压缩点早于已加载窗口（刻度置于起点）";
  if (mark.offPage === "before") return `锚点在第 ${mark.anchorPage + 1} 页（本页刻度钳在左边界，点击跳页）`;
  if (mark.offPage === "after") return `锚点在第 ${mark.anchorPage + 1} 页（本页刻度钳在右边界，点击跳页）`;
  return "刻度=锚定记录槽位的右边界（与记录轨同序号坐标）";
}

// renderCompressionLane 渲染底部「压缩」轨：在会话推进位置标记历次压缩（刻度
// 点击开详情）；压缩点早于已加载窗口时钳到轴起点，锚点不在本页时钳到页边界并
// 标注锚点页码（点击跳页，不静默位移）。同一锚点上的多个刻度按 stack 朝轨道
// 内侧错位（x=100 向左、其余向右），保证每一个刻度都能被点到。
function renderCompressionLane(marks) {
  const bar = marks.map((mark, index) => {
    const title = `压缩 #${mark.version} · ${mark.reasonLabel}${mark.messagesBefore > 0 ? ` · ${formatNumber(mark.messagesBefore)} 条` : ""}${mark.tokens > 0 ? ` · 约 ${formatNumber(mark.tokens)} tokens` : ""} · ${mark.timeLabel} · ${compressionMarkWhere(mark)} · 点击查看详情`;
    const shift = mark.stack ? (mark.x >= 100 ? -1 : 1) * mark.stack * 6 : 0;
    const offset = shift ? `;transform:translateX(calc(-50% ${shift > 0 ? "+" : "-"} ${Math.abs(shift)}px))` : "";
    return `<button type="button" class="axis-segment is-compress" style="--x:${mark.x.toFixed(3)}%${offset}" data-compact-idx="${index}" title="${escapeHtml(title)}" aria-label="${escapeHtml(`压缩 #${mark.version}`)}"><span>#${escapeHtml(String(mark.version))}</span></button>`;
  }).join("");
  return `<div class="context-axis-lane is-compress" aria-label="上下文压缩刻度（模型侧把旧段折叠为摘要的位置，对话原文仍保留）">
    <span class="axis-lane-label" title="软阈值达峰时把窗口外旧轮次折叠为栈顶摘要；刻度与记录轨共用序号坐标，标记压缩发生时会话推进到的位置"><span>压缩</span><span class="axis-lane-count">×${marks.length}</span></span>
    <div class="axis-lane-bar is-meta">${bar}</div>
  </div>`;
}

// contextAxisWeight 返回记录的相对体量（工具按输出/输入字符数，其余按内容+
// 推理字符数，最小 1）。体量不再参与轴几何（【规则 3】：位置只看序号），只用
// 于块 tooltip 与详情这类"这块有多大"的读数。
export function contextAxisWeight(record) {
  if (record.kind === "tool") {
    return Math.max(Number(record.totalChars) || String(record.output || "").length || String(record.input || "").length, 1);
  }
  return Math.max(String(record.output || "").length + String(record.reasoning || "").length, 1);
}

// renderAxisPageInfo 轴头的分页反馈：页码 / 本页序号区间 / 总量 / 页大小（并在
// 文案里说明交互方式）。停在最早一页且 hasMore 时明确写出"更早的回合尚未加载"，
// 避免用户以为轴丢了早期内容。data-* 给视图与测试一个稳定读取点。
function renderAxisPageInfo(window, options) {
  const range = window.end > window.start ? `第 ${window.start + 1}–${window.end} 条` : "本页为空";
  const earlier = options.hasMore && window.page === 0
    ? " · 更早的回合尚未加载（继续上翻可请求加载）"
    : "";
  return `<span class="context-axis-page" data-axis-page="${window.page}" data-axis-page-count="${window.pageCount}" data-axis-page-size="${window.pageSize}" data-axis-page-start="${window.start}" data-axis-page-end="${window.end}" data-axis-has-more="${options.hasMore ? "1" : "0"}">第 ${window.page + 1}/${window.pageCount} 页 · ${range} · 共 ${window.total} 条 · 每页 ${window.pageSize} 条（滚轮翻页，Shift+滚轮调页大小）${earlier}</span>`;
}

// renderAxisBlock 渲染一个记录块：x 与宽都来自序号槽位（【规则 3】）。tooltip
// 把"为什么在这里"写全：轨=类型、第几轮、本页第几槽、全局第几条；体量只作为
// 读数出现并显式标注"不参与位置"，避免读成第二套位置语义。
function renderAxisBlock(block, window) {
  const record = block.record;
  const kind = record.kind || "notice";
  const label = kind === "tool" ? (record.toolName || record.name) : trajectoryKindLabel(kind);
  const statusClass = statusClassName(record.status);
  const wide = block.width >= AXIS_WIDE_MIN_SLOT_PERCENT ? " is-wide" : "";
  const slot = block.index - window.start + 1;
  const turn = block.turn > 0 ? `第 ${block.turn} 轮` : "轮次开场";
  const title = `${trajectoryKindLabel(kind)}轨 · ${label} · ${trajectorySize(record)} · 体量 ${contextAxisWeight(record)} 字符（不参与位置） · ${turn} · 本页第 ${slot} 槽 / 共 ${window.capacity} 槽 · 全局第 ${block.index + 1} 条 · 点击定位轨迹行`;
  return `<button type="button" class="axis-segment is-${escapeHtml(kind)} ${statusClass}${wide}" style="--x:${block.x.toFixed(3)}%;--w:${block.width.toFixed(3)}%" data-trajectory-key="${escapeHtml(record.key)}" data-axis-index="${block.index}" data-axis-slot="${slot}" title="${escapeHtml(title)}" aria-label="${escapeHtml(label)}"><span>${escapeHtml(label)}</span></button>`;
}

// renderAxisLane 渲染一条记录轨：轨标签（title 带"这一轨是什么"的一句话）+
// 本页落在该轨的块。空轨保留并标 is-empty——"这一段没有该类块"是看得见的
// 结论，比让轨道消失更好读。
function renderAxisLane(lane, window) {
  const blocks = lane.blocks.map(block => renderAxisBlock(block, window)).join("");
  const hint = `${lane.label}：${lane.why}${lane.empty ? "（本页无块）" : ""}`;
  return `<div class="context-axis-lane is-${escapeHtml(lane.kind)}${lane.empty ? " is-empty" : ""}" aria-label="${escapeHtml(lane.label)}${lane.empty ? "（本页无块）" : ""}">
    <span class="axis-lane-label" title="${escapeHtml(hint)}">${trajectoryKindIcon(lane.kind, 10)}<span>${escapeHtml(lane.label)}</span></span>
    <div class="axis-lane-bar">${blocks}</div>
  </div>`;
}

// renderContextAxis 渲染轨迹视图顶部的上下文轴——多线谱（分轨）布局：
// 轨 = 响应类型（固定顺序见 AXIS_LANES 与文件头【规则 1】），块位置 = 页内序号
// 槽位（【规则 3】），一屏一页（【规则 4】）。extras 可附加两侧元数据轨与分页：
//  - prefixLayers（Bridge.PromptLayers）：顶部「前缀注入」轨——每次请求前置的
//    system 前缀层，横跨整轴（段宽 = 层文本占比，不参与记录序号坐标）；
//  - compactions（Snapshot.Task.ContextCompactions）：底部「压缩」轨——与记录轨
//    同序号坐标的压缩刻度（锚点不在本页时钳到页边界并标注页码）；
//  - page / pageSize：分页窗口（视图本地状态；过滤切换、详情开关、重渲染都保持）；
//  - notice：瞬时反馈（页码/页大小变更、更早内容未加载…）。
// 普通块点击由视图定位到对应轨迹行；元数据块点击开轴下方详情（view 侧）。
export function renderContextAxis(records = [], extras) {
  const options = normalizeAxisExtras(extras);
  const prefixSegments = prefixLayerSegments(options.prefixLayers);
  const view = axisBlocks(records, { page: options.page, pageSize: options.pageSize });
  const marks = compactionMarks(records, options.compactions, { page: view.window.page, pageSize: view.window.pageSize });
  if (!records.length && !prefixSegments.length && !marks.length) {
    return '<div class="context-axis-empty">暂无上下文轴数据</div>';
  }
  const lanes = view.lanes.map(lane => renderAxisLane(lane, view.window)).join("");
  const hints = [`横轴=页内序号槽位（每块 1 槽，本页 ${view.window.capacity} 槽）`];
  if (prefixSegments.length) hints.push("前缀注入=层文本占比");
  if (marks.length) hints.push(`压缩 ×${marks.length}（刻度可点击）`);
  hints.push("点击块定位轨迹行 · 滚轮翻页 · Shift+滚轮调页大小");
  return `<div class="context-axis" role="group" aria-label="上下文轴：按响应类型分轨，横轴为页内记录序号（非时间轴、非体量轴）">
    <div class="context-axis-head"><strong>上下文轴</strong>${renderAxisPageInfo(view.window, options)}<span class="context-axis-note" data-axis-note>${escapeHtml(options.notice)}</span><span class="context-axis-hint">${escapeHtml(hints.join(" · "))}</span></div>
    <div class="context-axis-track">${prefixSegments.length ? renderPrefixLane(prefixSegments) : ""}${lanes}${marks.length ? renderCompressionLane(marks) : ""}</div>
  </div>`;
}

// 表头行（Network 面板的列定义）。
const TRAJECTORY_COLUMNS = ["时间", "类型", "名称", "状态", "耗时", "大小"];

// renderTrajectoryTable 渲染轨迹表格（Network 风格）：表头 + 记录行。
// 返回 { html, items, payloads }：
//  - html     整表 HTML（不含过滤条/摘要）
//  - items    [{ key, html }]（keyed reconcile 用）
//  - payloads 完整 IN/OUT payload Map（复制/展开用）
export function renderTrajectoryTable(records) {
  const payloads = new Map();
  if (!records.length) {
    const html = `<div class="trajectory-empty">${escapeHtml(emptyTrajectoryText())}</div>`;
    return { html, items: [], payloads };
  }
  const head = `<div class="trajectory-row is-head" role="row">
    ${TRAJECTORY_COLUMNS.map(label => `<span>${escapeHtml(label)}</span>`).join("")}
  </div>`;
  const items = records.map(record => {
    const key = record.key;
    const html = renderTrajectoryRow(record, key, payloads);
    return { key, html };
  });
  return { html: head + items.map(item => item.html).join(""), items, payloads };
}

// renderTrajectoryRow 渲染一条轨迹记录行：主行（时间/类型/名称/状态/耗时/
// 大小）+ 可展开详情（IN/OUT 面板）。记录数据全部 escape 或进入 payload
// Map，不做未受控 HTML 注入。
export function renderTrajectoryRow(record, key, payloads) {
  const statusClass = statusClassName(record.status);
  const statusLabel = statusLabelOf(record.status);
  const duration = record.duration ? formatDuration(record.duration) : (record.status === "running" ? "运行中" : "");
  const size = trajectorySize(record);
  const input = prettyValue(record.input);
  const output = prettyValue(record.output);
  const inputKey = `${key}-in`;
  const outputKey = `${key}-out`;
  payloads.set(inputKey, input);
  payloads.set(outputKey, output);
  if (record.reasoning) {
    payloads.set(`${outputKey}-think`, String(record.reasoning));
  }

  const detail = record.kind === "tool"
    ? renderTrajectoryDetail({ input, output, inputKey, outputKey, record, statusClass })
    : renderTrajectoryTextDetail({ output, outputKey, record });

  return `<details class="trajectory-row ${statusClass}" data-trajectory-key="${escapeHtml(key)}" data-trajectory-kind="${escapeHtml(record.kind)}">
    <summary class="trajectory-row-main" role="row">
      <time datetime="${escapeHtml(record.startedAt || "")}">${escapeHtml(formatTime(record.startedAt))}</time>
      <span class="trajectory-kind is-${escapeHtml(record.kind)}">${trajectoryKindIcon(record.kind)}${escapeHtml(trajectoryKindLabel(record.kind))}</span>
      <strong class="trajectory-name" title="${escapeHtml(record.name)}">${escapeHtml(record.name)}</strong>
      <span class="trajectory-status ${statusClass}">${escapeHtml(statusLabel)}</span>
      <span class="trajectory-duration">${escapeHtml(duration)}</span>
      <span class="trajectory-size">${escapeHtml(size)}</span>
    </summary>
    <div class="trajectory-detail">${detail}</div>
  </details>`;
}

// renderTrajectoryDetail 工具记录详情：IN/OUT 双栏（复用 io-panel 类名，
// 与对话工具卡片同一套复制/展开/result_ref 交互契约）。
function renderTrajectoryDetail({ input, output, inputKey, outputKey, record, statusClass }) {
  const inputView = limitText(input, 1400, 28);
  const outputView = limitText(output, 4000, 40);
  const ref = record.resultRef || "";
  const note = record.truncated && ref
    ? `<span class="io-note">预览 ${outputView.total} 字符 · 全文 ${record.totalChars} 字符（默认折叠，点击加载）</span>`
    : outputView.truncated
      ? `<span class="io-note">预览 ${outputView.total} 字符</span>`
      : "";
  const fullButton = ref && record.truncated
    ? `<button class="io-expand" type="button" data-load-ref="${escapeHtml(ref)}" title="加载完整输出（按需拉取，默认折叠）">${svgExpand()} <span>加载完整输出</span></button>`
    : outputView.truncated
      ? `<button class="io-expand" type="button" data-expand="${outputKey}" title="展开完整内容">${svgExpand()} <span>+${outputView.hidden} chars</span></button>`
      : "";
  const outBody = outputView.truncated
    ? `<details class="io-collapse"><summary><span>查看输出</span><span class="io-collapse-meta">${escapeHtml(String(outputView.total))} chars</span></summary><pre>${escapeHtml(outputView.preview)}</pre></details>`
    : `<pre>${escapeHtml(outputView.preview)}</pre>`;
  return `${renderThinkPanel(record, outputKey)}<div class="trajectory-io-grid">
    <section class="io-panel" data-payload="${inputKey}">
      <header><span class="io-label">IN</span><span class="io-meta">${inputView.total} chars</span><button class="icon-button subtle" type="button" data-copy="${inputKey}" title="复制输入" aria-label="复制输入">${svgCopy()}</button></header>
      <pre>${escapeHtml(inputView.preview)}</pre>
    </section>
    <section class="io-panel ${statusClass === "is-error" ? "io-error" : ""}" data-payload="${outputKey}">
      <header><span class="io-label">OUT</span><span class="io-meta">${outputView.total} chars</span><button class="icon-button subtle" type="button" data-copy="${outputKey}" title="复制输出" aria-label="复制输出">${svgCopy()}</button></header>
      ${outBody}
      ${note}
      ${fullButton}
    </section>
  </div>`;
}

// renderTrajectoryTextDetail 非工具记录（input/llm/error/notice）详情：
// renderThinkPanel 渲染 THINK 面板（模型推理内容）。工具步骤与文本记录共用：
// 持久化里"这一步在想什么"挂在发起工具调用的那条消息上，两条路径都必须显示，
// 否则轨迹上只有工具痕迹、看不到推理。
function renderThinkPanel(record, outputKey) {
  const think = String(record?.reasoning || "").trim();
  if (!think) return "";
  const thinkKey = `${outputKey}-think`;
  const thinkView = limitText(think, 4000, 40);
  return `<section class="io-panel trajectory-think" data-payload="${thinkKey}">
        <header><span class="io-label">THINK</span><span class="io-meta">${thinkView.total} chars</span><button class="icon-button subtle" type="button" data-copy="${thinkKey}" title="复制思考内容" aria-label="复制思考内容">${svgCopy()}</button></header>
        <pre>${escapeHtml(thinkView.preview)}</pre>
        ${thinkView.truncated ? `<button class="io-expand" type="button" data-expand="${thinkKey}" title="展开完整内容">${svgExpand()} <span>+${thinkView.hidden} chars</span></button>` : ""}
      </section>`;
}

// 单栏全文（同样走 payload Map + 折叠预览）。
function renderTrajectoryTextDetail({ output, outputKey, record }) {
  const view = limitText(output, 4000, 40);
  const note = view.truncated ? `<span class="io-note">预览 ${view.total} 字符</span>` : "";
  const fullButton = view.truncated
    ? `<button class="io-expand" type="button" data-expand="${outputKey}" title="展开完整内容">${svgExpand()} <span>+${view.hidden} chars</span></button>`
    : "";
  const thinkPanel = renderThinkPanel(record, outputKey);
  const body = view.truncated
    ? `<details class="io-collapse"><summary><span>查看内容</span><span class="io-collapse-meta">${escapeHtml(String(view.total))} chars</span></summary><pre>${escapeHtml(view.preview)}</pre></details>`
    : `<pre>${escapeHtml(view.preview)}</pre>`;
  return `<div class="trajectory-text-panel">
    ${thinkPanel}
    <section class="io-panel ${record.status === "error" ? "io-error" : ""}" data-payload="${outputKey}">
      <header><span class="io-label">BODY</span><span class="io-meta">${view.total} chars</span><button class="icon-button subtle" type="button" data-copy="${outputKey}" title="复制内容" aria-label="复制内容">${svgCopy()}</button></header>
      ${body}
      ${note}
      ${fullButton}
    </section>
  </div>`;
}

// 轨迹大小列：截断记录用归档总字符，其余用可见输出长度（KB 展示）。
function trajectorySize(record) {
  const chars = record.totalChars || String(record.output || "").length;
  if (chars <= 0) return "—";
  if (chars < 1024) return `${chars} B`;
  return `${(chars / 1024).toFixed(chars < 10240 ? 1 : 0)} KB`;
}

function emptyTrajectoryText() {
  return "暂无轨迹记录。轨迹来自当前会话可见窗口（对话区消息），工具调用、LLM 回复、错误与系统通知会按响应类型列在这里。";
}

// ── 本地工具（与 components.js 保持同一呈现契约；纯函数，无模块依赖）──

export function escapeHtml(value = "") {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

function prettyValue(value) {
  const text = String(value || "").trim();
  if (!text) return "";
  try {
    return JSON.stringify(JSON.parse(text), null, 2);
  } catch {
    return text;
  }
}

function limitText(value, maxChars, maxLines) {
  const text = String(value || "");
  const lines = text.split("\n");
  let preview = lines.slice(0, maxLines).join("\n");
  if (preview.length > maxChars) preview = preview.slice(0, maxChars);
  const truncated = preview.length < text.length;
  return { preview, truncated, hidden: Math.max(text.length - preview.length, 0), total: text.length };
}

function formatDuration(duration) {
  const milliseconds = Number(duration) / 1e6;
  if (!Number.isFinite(milliseconds) || milliseconds <= 0) return "";
  return milliseconds >= 1000 ? `${(milliseconds / 1000).toFixed(1)}s` : `${Math.round(milliseconds)}ms`;
}

function formatTime(iso) {
  if (!iso) return "—";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false });
}

function statusClassName(status) {
  if (status === "error" || status === "failed") return "is-error";
  if (status === "running" || status === "pending") return "is-running";
  if (status === "success") return "is-success";
  return "is-info";
}

function statusLabelOf(status) {
  if (status === "error" || status === "failed") return "ERR";
  if (status === "running" || status === "pending") return "RUN";
  if (status === "success") return "OK";
  return "—";
}

function svgCopy() {
  return '<svg class="icon" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="8" y="8" width="11" height="11" rx="2"/><path d="M16 8V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h3"/></svg>';
}

function svgExpand() {
  return '<svg class="icon" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m8 3-5 5M3 3v5h5M16 3l5 5M21 3v5h-5M8 21l-5-5M3 21v-5h5M16 21l5-5M21 21v-5h-5"/></svg>';
}
