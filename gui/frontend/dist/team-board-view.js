import { escapeHtml } from "./components.js";

// team-board-view.js 是「团队（Teamwork）」看板的**纯渲染件**（不碰 DOM，便于 node:test）。
//
// 形状（2026-10-08 · 侧边栏瘦身 + 详情页下钻）：侧边栏只报**最小可读集**，细节一律交给
// **详情页**（用户点开才看）。
//
//   ┌ 侧边栏（#team-board-view）──────────────────────────────────────┐
//   │ 看板头      team_id · 版本 · 一行计数                            │
//   │ 里程碑格栅  一格 = 一层依赖（**不是时间**）                       │
//   │              M1 基础能力建设  4/6  running  🔓     [▢▣▣▢]        │
//   │                WI-1 WI-2 WI-3 …                                  │
//   │                内容摘要…                                          │
//   │ teammate    role · 状态 · 负责 n 件事 · 工作区（一行）            │
//   │ 审计        最近 4 条（一行一条）                                 │
//   └──────────────────────────────────────────────────────────────────┘
//
// 四条**必须写下来**的口径（不写下来，形状会自己长回去）：
//
//  1. **横轴是依赖槽位，不是时间**（沿用 2026-10-08 wi-gantt 的口径，缩到里程碑一层）：
//     计划里没有工时事实，一格一律 1 层依赖 —— 不编"开始/结束日期"，也不按条数假装工期。
//     格栅只是**借甘特的形状**把"谁在谁之后、一块占多宽"摆成看得见的横排。
//     slot = max(end(deps))（finish→start），里程碑的跨度 = 名下工作项的 min(slot)..max(end)。
//  2. **不画箭头**：依赖关系不画成几何。里程碑的屏障（depends_on）与工作项的 DAG 在详情页的
//     「依赖」页签里逐条列全（每条前驱带**状态**，并且反向列出**被谁依赖**）。一句话说得清
//     的事不画成折线——尤其在只有 ~360px 的产品右栏里，折线只会把行挤没。
//     里程碑行上留下的唯一提示是 🔒/🔓（含"等 X + Y"）：它回答"这一块现在进不进得去"，
//     那正是屏上要一眼看到的那件事。
//  3. **三种详情页共用一套壳**（页签 + 返回栈）：里程碑 / Work Item / teammate。
//     **Work Item 详情页被另外两种页复用**（里程碑页的「Work Item」页签、teammate 页的
//     「负责的 Work Item」页签都点进**同一个** renderWorkItemDetail）——一处定义，两处下钻，
//     不各写一份"差不多"的页面。
//  4. **判定只有一个判据**：状态折算（effStatus / milestoneEff）、槽位（slotOf）、屏障放行
//     （hasUndoneDep）在这份文件里各只有一处；侧边栏行与详情页都读它，不各算一遍。
//
// 数据源三份，全部是后端只读投影——前端**没有任何写入口**（渲染结果不回写后端）：
//   1. plan   计划本体：sessionstore.TeamworkPlan（team_id / version / members /
//              milestones / work_items）。**顺序的唯一事实是 depends_on**（里程碑之间看
//              milestones[].depends_on，里程碑内部看 work_items[].depends_on），本件不从
//              调用姿势猜顺序。
//   2. jobs   作业投影：与工作表格同源的作业行（handle / state / exit_code / bytes /
//              node / scope.subject）。注意 jobs I-4：**句柄只是投影**，进程重启后一律视为过期，
//              真值以 Observe 的返回为准。（本件用它画详情页的「作业」页签。）
//   3. events 审计流水：sessionstore.TeamworkEvent[]（plan / dispatch / join / milestone / retire）。
//
// **没有阶段（stages）这个概念**（2026-10-04）：计划里只有里程碑 + 工作项，看板也只按
// 这两层画。老口径的计划即便在 JSON 里还带着 stages，它也不会被当成编排形状。

const TEXT_LIMIT = 160;
const CONTENT_LIMIT = 240;
// AUDIT_LIMIT 是侧边栏审计节的行数：审计是"最近发生了什么"的尾巴，不是台账——全量在
// 详情页的「动态」页签里（按里程碑 / 工作项 / teammate 各自过滤过）。
const AUDIT_LIMIT = 4;

// ROLE_PALETTE_SIZE 是 teammate 色板的格数（--team-dag-role-0..5）。
const ROLE_PALETTE_SIZE = 6;

// ROLE_SLOT 是 teammate 配色的**登记处**：按 role 首次出现的次序分配，append-only。
//
// 为什么是模块级的登记处、而不是每次从 plan 现算：同一位 teammate 的颜色不许因为
// 「重渲染 / 推进一轮 / 换主题」而变（口径 3）。现算的话，计划里新增一位 teammate 就会把
// 后面所有人的 slot 挤一位，颜色跟着漂——那是"颜色在描述轮次"，不是"颜色在描述是谁"。
// 登记的唯一输入是 role 名（不是时间戳、不是随机数、也不是任何自增 id 写进行里），
// 所以同一份输入连渲两次，结果字符串逐字相同（幂等，口径 8）。
const ROLE_SLOT = new Map();

// roleSlotOf 返回 role 的登记序号（append-only，从 0 起）。slot >= ROLE_PALETTE_SIZE 表示
// 色板已经回绕（第 7 位 teammate 起会与前面某位同色）——渲染时给色点 / 格子叠一层**斜纹
// 第二通道**，免得两位撞色的 teammate 只靠色块分不出来（口径 11：保留 6 色，斜纹是防撞色，
// 不是装饰）。
//
// 口径校正（2026-10-07 · 独立验证 F4）：登记次序是「**首次进入渲染的次序**」，不是
// `plan.members[]` 的声明序 —— 本函数只有一个调用点（renderMilestoneRow 的 WI chip / 格子），
// 所以设计稿 README §2 那句「先扫 members[]，再补 work_items[].role」只对**稿子**成立，
// 对产品实现不成立。这里的取舍是**进程内稳定**（同一份计划重渲染 / 跨轮 / 换主题都不变色），
// 代价是与声明序无关。
export function roleSlotOf(role) {
  const name = String(role ?? "");
  if (!ROLE_SLOT.has(name)) ROLE_SLOT.set(name, ROLE_SLOT.size);
  return ROLE_SLOT.get(name);
}

// roleColorVar 把 role 折成色板里的令牌名（超出 6 个就回绕复用）。
export function roleColorVar(role) {
  return `--team-dag-role-${roleSlotOf(role) % ROLE_PALETTE_SIZE}`;
}

// roleWrapped 判这一位是否落在色板回绕之后再叠斜纹（防撞色）。
function roleWrapped(role) {
  return Boolean(role) && roleSlotOf(role) >= ROLE_PALETTE_SIZE;
}

// ── 里程碑（屏障）与工作项（格栅节点）：纯函数 ────────────────────────

// milestonesOf 取里程碑数组；milestoneStatus 缺省 pending（空 = pending，见 sessionstore）。
export function milestonesOf(plan) {
  return Array.isArray(plan?.milestones) ? plan.milestones.filter(item => item && item.id) : [];
}

export function milestoneStatus(milestone) {
  const status = String(milestone?.status || "").trim().toLowerCase();
  return status || "pending";
}

// milestoneEff 是**里程碑线框色的唯一判据**。里程碑的状态字面量只有三个
// （sessionstore：""=pending / active / done，见 teamwork.go 的校验白名单），比工作项少一个
// review 与 interrupted —— 后者是"某件事被中断"，里程碑没有这件事。
//
// 为什么不复用 effStatus：effStatus 认的字面量是工作项那一族（running/review/failed），
// 里程碑的 active 落进去会被判成 pending —— 一块正在推进的里程碑会被画成"还没开始"。
export function milestoneEff(milestone) {
  const status = milestoneStatus(milestone);
  if (status === "done") return "done";
  if (status === "active" || status === "running") return "running";
  if (status === "review") return "review";
  if (status === "failed" || status === "killed") return "failed";
  return "pending";
}

// milestoneDepsOf 取里程碑之间的**屏障**依赖（去空白、去空项）。
export function milestoneDepsOf(milestone) {
  return (Array.isArray(milestone?.depends_on) ? milestone.depends_on : [])
    .map(dep => String(dep || "").trim())
    .filter(Boolean);
}

// milestoneAfterOf 取里程碑的**判据**（after；leader 写的验收边，与屏障是两回事）。
export function milestoneAfterOf(milestone) {
  return (Array.isArray(milestone?.after) ? milestone.after : [])
    .map(dep => String(dep || "").trim())
    .filter(Boolean);
}

// workItemsOf 取计划里的工作项（扁平数组，每条自带 milestone）。
export function workItemsOf(plan) {
  return Array.isArray(plan?.work_items) ? plan.work_items.filter(item => item && item.id) : [];
}

// itemStatus 读工作项状态（空 = pending，与 sessionstore 同口径）。
export function itemStatus(item) {
  return String(item?.status || "").trim().toLowerCase() || "pending";
}

// effStatus 是**工作项线框色的唯一判据**（口径 2）：interrupted 先抬成红，再按 status 取色。
//
// 一条红不裂成两条：failed 与 killed 同色；interrupted === true 也走这条红——被中断是
// 「需要恢复、可以重派」，与「跑失败了」在颜色上是同一件事（文案上才分开）。
// 为什么必须有这一个判据：颜色若在几处各算一遍，格子、色点、chip 就会各说各话。
export function effStatus(item) {
  if (item?.interrupted === true) return "failed";
  const status = itemStatus(item);
  if (status === "failed" || status === "killed") return "failed";
  if (status === "review") return "review";
  if (status === "done") return "done";
  if (status === "running") return "running";
  return "pending";
}

// itemDepsOf 取工作项的**里程碑内**依赖（去空白、去空项）。
export function itemDepsOf(item) {
  return (Array.isArray(item?.depends_on) ? item.depends_on : [])
    .map(dep => String(dep || "").trim())
    .filter(Boolean);
}

// itemsOfMilestone 取一个里程碑名下的工作项（按后端给的顺序）。
export function itemsOfMilestone(plan, milestoneID) {
  const id = String(milestoneID || "");
  return workItemsOf(plan).filter(item => String(item.milestone || "") === id);
}

// orderMilestones 按 depends_on 屏障做拓扑排序 + 层号（层 = 最长依赖链上的位置）。
// 依赖指向不存在的里程碑时忽略该边；成环时把剩下的里程碑按原顺序接在末尾（层号归 0，
// cyclic=true）——宁可在看板上显形，也不静默丢里程碑。
export function orderMilestones(plan) {
  const milestones = milestonesOf(plan);
  const ids = new Set(milestones.map(milestone => String(milestone.id)));
  const deps = new Map(milestones.map(milestone => [String(milestone.id), milestoneDepsOf(milestone)]));
  const depth = new Map();
  const ordered = [];
  let frontier = milestones.filter(milestone => deps.get(String(milestone.id)).filter(dep => ids.has(dep)).length === 0);
  for (const milestone of frontier) depth.set(String(milestone.id), 0);
  while (frontier.length) {
    for (const milestone of frontier) ordered.push(milestoneEntry(plan, milestone, depth.get(String(milestone.id))));
    const next = [];
    for (const milestone of milestones) {
      const id = String(milestone.id);
      if (depth.has(id)) continue;
      const pending = deps.get(id).filter(dep => ids.has(dep) && !depth.has(dep));
      if (pending.length) continue;
      const level = Math.max(0, ...deps.get(id).filter(dep => ids.has(dep)).map(dep => (depth.get(dep) ?? 0) + 1));
      depth.set(id, level);
      next.push(milestone);
    }
    frontier = next;
  }
  for (const milestone of milestones) {
    if (!depth.has(String(milestone.id))) ordered.push(milestoneEntry(plan, milestone, 0, true));
  }
  return ordered;
}

function milestoneEntry(plan, milestone, depth, cyclic = false) {
  const id = String(milestone.id);
  const known = new Set(milestonesOf(plan).map(item => String(item.id)));
  return {
    id,
    name: String(milestone.name || "").trim(),
    depends_on: milestoneDepsOf(milestone),
    missing_deps: milestoneDepsOf(milestone).filter(dep => !known.has(dep)),
    after: milestoneAfterOf(milestone),
    content: String(milestone.content || "").trim(),
    status: milestoneStatus(milestone),
    // depth = 屏障**深度**（依赖链最长路径）；layer 由 ganttModel 另写成**拓扑序号**。
    // 两者在链式计划里恰好相同，在「两个互不依赖的里程碑」或成环时会分叉 —— 两个数都留着，
    // 详情页的「屏障层号」读 depth，格栅的逐块配色读 layer。
    depth,
    cyclic,
    items: itemsOfMilestone(plan, id),
  };
}

// orderWorkItems 按**里程碑内**依赖做拓扑排序 + 层号；blocked = 前置还没验收通过。
//
// blocked 不是"不能派"，而是"现在派会被闸门拒"——它与后端的判据同源（依赖必须 done），
// 于是看板说"被卡住"与编排面真的拒收是同一条事实，不会各说各话。
//
// `allByID` 是**全计划**的工作项索引（ganttModel 传进来；单独调用时缺省退回本里程碑）。
// 三个字段各说各的那一件事（独立验证 ③：以前一个 byID 身兼三职，"在别的里程碑里"被读成
// "不存在"——既写「依赖缺失」，又永久 `blocked`，而几何那边 `slotOf` 用的是全计划索引、
// 明明按跨里程碑算了槽位，同一件事三个说法）：
//   missing_deps = **全计划里根本不存在**的 id（真写错了，只有这一类才配叫"依赖缺失"）；
//   cross_deps   = 全计划里存在、但**不在本里程碑**的 id（编排口径只允许同里程碑内）；
//   blocked      = 按**全计划**的状态判：存在且 done 才算放行（取不到 = 没 done = 卡住）。
export function orderWorkItems(items, allByID = null) {
  const list = (Array.isArray(items) ? items : []).filter(item => item && item.id);
  const ids = new Set(list.map(item => String(item.id)));
  const byID = new Map(list.map(item => [String(item.id), item]));
  const scope = allByID instanceof Map ? allByID : byID;
  const depth = new Map();
  const ordered = [];
  let frontier = list.filter(item => itemDepsOf(item).filter(dep => ids.has(dep)).length === 0);
  for (const item of frontier) depth.set(String(item.id), 0);
  while (frontier.length) {
    for (const item of frontier) ordered.push(workItemEntry(item, byID, depth.get(String(item.id)), false, scope));
    const next = [];
    for (const item of list) {
      const id = String(item.id);
      if (depth.has(id)) continue;
      const pending = itemDepsOf(item).filter(dep => ids.has(dep) && !depth.has(dep));
      if (pending.length) continue;
      const level = Math.max(0, ...itemDepsOf(item).filter(dep => ids.has(dep)).map(dep => (depth.get(dep) ?? 0) + 1));
      depth.set(id, level);
      next.push(item);
    }
    frontier = next;
  }
  for (const item of list) {
    if (!depth.has(String(item.id))) ordered.push(workItemEntry(item, byID, 0, true, scope));
  }
  return ordered;
}

function workItemEntry(item, byID, depth, cyclic = false, scope = byID) {
  const deps = itemDepsOf(item);
  return {
    item,
    id: String(item.id),
    depth,
    cyclic,
    depends_on: deps,
    missing_deps: deps.filter(dep => !scope.has(dep)),
    cross_deps: deps.filter(dep => !byID.has(dep) && scope.has(dep)),
    blocked: deps.some(dep => itemStatus(scope.get(dep)) !== "done"),
  };
}

// slotOf 是 FS（finish→start）语义的槽位：无依赖 = 0，否则 max(slot(d) + dur(d))，dur 恒 1
// ——紧贴最后一个前驱的右端。成环走 seen 集合兜底当 0（不假造槽位）；**自指依赖不占槽位**
// ——它不表示任何先后关系，占一格只会让这一行凭空右移一格。
function slotOf(id, byID, seen = new Set()) {
  const key = String(id);
  const item = byID.get(key);
  if (!item || seen.has(key)) return 0;
  const deps = itemDepsOf(item).filter(dep => dep !== key && byID.has(dep));
  if (!deps.length) return 0;
  const next = new Set(seen);
  next.add(key);
  return Math.max(...deps.map(dep => slotOf(dep, byID, next) + 1));
}

// hasUndoneDep 是**屏障**的单一判据：depends_on 里只要有一个不是 done（含指向不存在的
// 里程碑）就算没放行。🔒/🔓 由它决定，详情页的「屏障」读数也由它决定——一处判定，两处显示。
function hasUndoneDep(frames, deps) {
  return (Array.isArray(deps) ? deps : []).some(dep => {
    const owner = frames.find(frame => frame.id === dep);
    return !owner || owner.status !== "done";
  });
}

// memberQueueOf 取某位 teammate 的工作项名称队列：优先用后端算好的 queue，缺失时按
// 工作项自行派生（**同一条判据**：未完成的工作项才占队列）。两种来源读法一致，
// 所以后端降级（旧投影）时看板不会显示成"这个人无事可做"。
export function memberQueueOf(plan, role) {
  const name = String(role || "");
  const member = (Array.isArray(plan?.members) ? plan.members : []).find(entry => entry && String(entry.role || "") === name);
  if (member && Array.isArray(member.queue)) return member.queue.map(entry => String(entry || "")).filter(Boolean);
  return workItemsOf(plan)
    .filter(item => String(item.role || "") === name && itemStatus(item) !== "done")
    .map(item => String(item.name || item.id));
}

// memberWorkOf 取某位 teammate **名下全部**工作项（含已销项）：侧边栏那行报的是"这个人负责
// 几件事、做完几件"，不是"还剩几件"——两个问题两个数，缺一个就读不出进度。
export function memberWorkOf(plan, role) {
  const name = String(role || "");
  return workItemsOf(plan).filter(item => String(item.role || "") === name);
}

// memberMessagesOf 取某位 teammate 的消息队列（尾插回执；后端给多少读多少）。
export function memberMessagesOf(plan, role) {
  const name = String(role || "");
  const member = (Array.isArray(plan?.members) ? plan.members : []).find(entry => entry && String(entry.role || "") === name);
  return Array.isArray(member?.messages) ? member.messages.filter(message => message && message.text) : [];
}

// memberStatusOf 取某位 teammate 的实时状态：**只有 running / free 两个值**
// （2026-10-04 用户口径）。
//
// 后端给什么认什么，但取值收敛到这两个：一个"没在干活的人"不该有 done / idle / review
// 三种写法（前端各写一遍就是三份口径）。后端缺失时按 free —— 缺状态字段是降级输入，
// 而"这个人此刻没在跑"是降级下最诚实的读法。
export function memberStatusOf(plan, role) {
  const name = String(role || "");
  const member = (Array.isArray(plan?.members) ? plan.members : []).find(entry => entry && String(entry.role || "") === name);
  const status = String(member?.status || "").trim().toLowerCase();
  if (status === "running") return "running";
  return "free";
}

// memberOf 取某位 teammate 的成员行（拿不到 = 这位不在编；调用方据此退场而不是画空壳）。
export function memberOf(plan, role) {
  const name = String(role || "");
  return (Array.isArray(plan?.members) ? plan.members : []).find(entry => entry && String(entry.role || "") === name) || null;
}

// memberCurrentSessionOf 反解"这位 teammate 此刻那件事的会话"：优先在跑的工作项，
// 其次等验收的，都没有就退到它最近开过工的那件事；一件都没开过 → 全空（**调用方不挂
// 入口**：teammate 的角色会话读面是群聊车道形状——main 车道 = 主会话整段，而它自己的
// 行从来不写在那里，退过去就是把主代理的会话冒充成这位的会话，见 renderTeamQueue）。
//
// 为什么前端也要算一份：后端已经给了 current_session_id / current_work_item（权威），
// 这里在同一条口径上做**降级兜底**——老快照（没有这两个字段）仍然要能点出正确的入口，
// 而不是退回"员工的长期历史会话"。两处都读同一份事实（工作项 + 状态），不存在第二份口径。
export function memberCurrentSessionOf(plan, role) {
  const name = String(role || "");
  const member = (Array.isArray(plan?.members) ? plan.members : []).find(entry => entry && String(entry.role || "") === name);
  const provided = String(member?.current_session_id || "").trim();
  if (provided) {
    return { session_id: provided, work_item: String(member?.current_work_item || ""), name: "" };
  }
  let running = null;
  let review = null;
  let last = null;
  for (const item of workItemsOf(plan)) {
    if (String(item.role || "") !== name) continue;
    const session = String(item.session_id || "").trim();
    if (!session) continue;
    const status = itemStatus(item);
    if (status === "running") running = item;
    else if (status === "review" && !review) review = item;
    else last = item;
  }
  const chosen = running || review || last;
  if (!chosen) return { session_id: "", work_item: "", name: "" };
  return { session_id: String(chosen.session_id || ""), work_item: String(chosen.id || ""), name: String(chosen.name || "") };
}

// teammateSessionEntry 决定"这位的会话"该开什么——**两条入口共用这一份判定**
// （看板的 teammate 行 + Agent Team 面板成员行）。
//
// 判据只有一条事实：这位是不是这份团队计划里的 teammate。
//   - 是，且有"这件事自己的会话" → `{kind:"live", session_id, work_item}`：开实时读面
//     （一 Work Item 一套 Session，那一轮活跑在**进程内执行面**上，正文只能实时读）；
//   - 是，但没有（还没派活 / 已验收销项）→ `{kind:"none", reason}`：**不打开**。
//     为什么"不打开"而不是"退回角色会话"（2026-10-04 用户口径修正：查看 teammate 的
//     会话"总是看到主代理的会话"）：teammate 的角色会话读面是**群聊车道**形状——main
//     车道 = **主会话整段**，而 teammate 自己那条车道在存储里从来没有行（worker 回合不写
//     存储）。退过去，用户看到的就是主代理的会话被当成这位的会话。宁可不给入口。
//   - 不是 teammate（goal-a2a 的 tl 等真有自己角色的会话）→ `{kind:"role"}`：照旧走角色会话。
export function teammateSessionEntry(plan, role) {
  const name = String(role || "").trim();
  if (!name) return { kind: "none", reason: "角色名为空" };
  const current = memberCurrentSessionOf(plan, name);
  if (current.session_id) {
    return { kind: "live", session_id: current.session_id, work_item: current.work_item, name: current.name };
  }
  if (memberOf(plan, name)) {
    return { kind: "none", reason: noOwnSessionNote(name) };
  }
  return { kind: "role" };
}

// noOwnSessionNote 是"这一位此刻没有自己的会话"的同一句话（看板上是 title，详情页上是提示）：
// 一句话只写一次，用户在两处读到的是同一份解释。
function noOwnSessionNote(role) {
  return `${role} 这一位此刻没有自己的会话：teammate 的每一件事各自一套 Session（一 Work Item 一套），没有在跑或待验收的工作项就没有可看的正文`;
}

// summarizeTeam 是看板头部的一行计数（里程碑 / 工作项 / 在编 / 作业）。
//
// 只报事实，不给阶段数——阶段口径已退场，工作项才是"排了多少活、走了多少"的单位。
export function summarizeTeam(plan, jobs) {
  const members = Array.isArray(plan?.members) ? plan.members.filter(Boolean) : [];
  const milestones = milestonesOf(plan);
  const items = workItemsOf(plan);
  const statuses = items.map(itemStatus);
  const jobList = (Array.isArray(jobs) ? jobs : []).filter(Boolean);
  return {
    members: members.length,
    milestones_total: milestones.length,
    milestones_done: milestones.filter(milestone => milestoneStatus(milestone) === "done").length,
    items_total: items.length,
    items_done: statuses.filter(status => status === "done").length,
    items_running: statuses.filter(status => status === "running").length,
    items_review: statuses.filter(status => status === "review").length,
    items_failed: statuses.filter(status => status === "failed").length,
    jobs_running: jobList.filter(job => jobState(job) === "running").length,
    jobs_failed: jobList.filter(jobFailed).length,
    jobs_done: jobList.filter(job => jobState(job) === "done").length,
  };
}

// 作业终态：failed / killed 都算「这条作业没走通」，不把被杀当成还在跑。
const FAILED_JOB_STATES = new Set(["failed", "killed"]);

// jobState / jobFailed 是作业行的状态读法：缺字段一律当未知，不当作 done。
export function jobState(job) {
  return String(job?.state || "").trim().toLowerCase();
}

export function jobFailed(job) {
  return FAILED_JOB_STATES.has(jobState(job));
}

// ── 里程碑格栅（不是时间轴）─────────────────────────────────────
//
// 几何只有两样东西，都是**纯 CSS 算式**（渲染件量不到 rect，也不需要量）：
//   x = 槽位 × --team-dag-slot-w（左端）+ 1 格宽（条宽）；
//   格子底纹 = repeating-linear-gradient 每 --team-dag-slot-w 一条竖线。
// 上一版在这里做过"真甘特"的边与箭头（x 算式 + y 实测），随口径 2（不画箭头）整条退场：
// 行高不再需要实测，`layoutTeamGanttEdges` / `scheduleTeamGanttLayout` / 边段几何全部删除
// ——留着就是没人调的死代码，而且会把"已经不需要的第二套几何"留在文件里。

// ganttModel 把计划排成**里程碑格栅**模型：一行一个里程碑（不再一行一个工作项——工作项
// 收进里程碑的 WI 编号串与格子里）。`slot` / `dur` = 工作项那一格的起点与长度（恒 1 格），
// 里程碑的 `sum` = 名下工作项的 min(slot)..max(end)。
//
// 行序 = 里程碑屏障的拓扑序（orderMilestones）；里程碑内工作项按里程碑内 DAG 排
// （orderWorkItems），WI 编号串按这个序读，才与"被依赖者在前"一致。
export function ganttModel(plan) {
  const byID = new Map(workItemsOf(plan).map(item => [String(item.id), item]));
  const ordered = orderMilestones(plan);
  const frames = ordered.map((frame, layer) => ({
    ...frame,
    layer,
    tone: layer % 5,
    locked: hasUndoneDep(ordered, frame.depends_on),
    rows: [],
    sum: { s: 0, e: 0, empty: true },
    done: 0,
    total: 0,
  }));
  for (const frame of frames) {
    // 全计划 byID 传进去：行上的「依赖缺失 / 跨里程碑依赖 / 被卡住」三条判据都要看全计划
    // （几何那边的 slotOf 用的也是它），否则同一件事会被说成三个样（独立验证 ③）。
    frame.rows = orderWorkItems(frame.items, byID).map(entry => {
      const slot = slotOf(entry.id, byID);
      // depth 也用几何槽位：跨里程碑的工作项依赖会让 orderWorkItems 的里程碑内层号与几何
      // 位置分叉，而格子的位置才是这一节要说的那件事。
      return { ...entry, milestone_id: frame.id, layer: frame.layer, slot, dur: 1, end: slot + 1, depth: slot };
    });
    frame.total = frame.rows.length;
    frame.done = frame.rows.filter(row => itemStatus(row.item) === "done").length;
  }
  const rows = frames.flatMap(frame => frame.rows);
  const frameByID = new Map(frames.map(frame => [frame.id, frame]));
  for (const frame of frames) {
    if (!frame.rows.length) continue;
    frame.sum = {
      s: Math.min(...frame.rows.map(row => row.slot)),
      e: Math.max(...frame.rows.map(row => row.end)),
      empty: false,
    };
  }
  // 空里程碑不画零宽条：它落在屏障前驱汇总条的**右端**（无前驱 → 槽 0）——那一格正是
  // 「这一块还没排活、但它接在谁后面」读得出来的地方。
  for (const frame of frames) {
    if (!frame.sum.empty) continue;
    let at = 0;
    for (const dep of frame.depends_on) {
      const owner = frameByID.get(dep);
      if (owner) at = Math.max(at, owner.sum.e);
    }
    frame.sum = { s: at, e: at, empty: true };
  }
  let slots = 1;
  for (const row of rows) slots = Math.max(slots, row.end);
  for (const frame of frames) slots = Math.max(slots, frame.sum.e);
  return { frames, rows, slots };
}

// renderTeamGantt 是**里程碑**那一节：刻度尺（sticky）→ 一行一个里程碑（左侧读名字 / 状态 /
// 进度 / WI 编号 / 内容摘要，右侧是格子）。横轴是**依赖槽位，不是时间**；刻度尺上就写着
// 这句话，免得读的人自己脑补出工期。
export function renderTeamGantt(plan) {
  const model = ganttModel(plan);
  if (!model.frames.length) return "";
  const ticks = [];
  for (let i = 0; i <= model.slots; i += 1) {
    ticks.push(`<span class="team-dag-tick" data-tick="${i}" style="--i:${i}"><i></i><b>${i}</b></span>`);
  }
  return `<section class="team-section" data-team-gantt>
      <div class="team-section-title"><span>里程碑</span><span class="chip team-count" title="共 ${model.frames.length} 个里程碑">${model.frames.length}</span><span class="team-section-hint">点里程碑看详情</span></div>
      <div class="team-dag-scroll">
        <div class="team-dag-content" style="--team-dag-slots:${model.slots}">
          <div class="team-dag-ruler" data-role="ruler">
            <span class="team-dag-ruler-label">里程碑</span>
            <div class="team-dag-ruler-plot">${ticks.join("")}<span class="team-dag-ruler-basis">横轴 = 依赖槽位（非时间）· 1 格 = 1 层依赖 · 每格一件事</span></div>
          </div>
          <div class="team-dag-ms-rows">${model.frames.map(renderMilestoneRow).join("")}</div>
        </div>
      </div>
    </section>`;
}

// renderMilestoneRow 渲染**一个里程碑 = 一行**：
//   左列（可点）读 id · name / status chip / 进度 n/m / 🔒🔓 / 名下 WI 编号串 / 内容摘要；
//   右列（格栅）读跨度：一格一件事（格子里是 teammate 色 + 状态描边），底下垫一条汇总条。
//
// 两条颜色通道在这一行里正交（口径 3/13）：**描边 / 格线上沿 = 状态**（data-eff，pending 另加
// 虚线），**格子填充 + WI chip 色点 = 归属（teammate）**；行文字一律中性色，不拿状态色染整行。
//
// 里程碑自己的颜色是**逐框轮换的淡色**（--team-dag-ms-tone-0..4），不吃状态色、不吃 teammate
// 色——它是"这是第几块"的读法，不是"这块好不好"的读法。
function renderMilestoneRow(frame) {
  const status = frame.status;
  const eff = milestoneEff(frame);
  const sum = frame.sum;
  const wis = frame.rows.map(row => {
    const item = row.item;
    const role = String(item.role || "").trim();
    const skin = role ? ` style="--team-dag-role-color:var(${roleColorVar(role)})"` : "";
    const tip = `${String(item.id || "")} ${String(item.name || "")} · ${itemStatus(item)}${role ? " · @" + role : ""} · 槽 ${row.slot}`;
    return `<span class="chip team-wi" data-eff="${escapeHtml(effStatus(item))}" data-item="${escapeHtml(String(item.id || ""))}"${skin} title="${escapeHtml(tip)}"><i class="team-dag-dot"></i>${escapeHtml(String(item.id || ""))}</span>`;
  }).join("");
  // 同槽并行：两件工作项落在同一格（slot 相同）是合法的（`b→a`、`c→d` 这种手绘形状里就有）。
  // **一格里挤几件就分成几小格**：直接叠着画的话，后画的那件会把先画的那件整个盖住——
  // 像素量测现场：chain 夹具里 wi-render 是 failed+interrupted，红色描边一个像素都读不到
  // （它被同槽的 wi-cases 盖住了）。所以每一件拿到 --n（这一格几件）与 --k（第几件），
  // 左端与宽度都由 CSS 算（渲染件照旧一个像素都不写）。
  const slotGroups = new Map();
  for (const row of frame.rows) {
    const key = String(row.slot);
    if (!slotGroups.has(key)) slotGroups.set(key, []);
    slotGroups.get(key).push(String(row.item.id || ""));
  }
  const cells = frame.rows.map(row => {
    const item = row.item;
    const role = String(item.role || "").trim();
    const wrapped = roleWrapped(role) ? " is-wrapped" : "";
    const skin = role ? `;--team-dag-role-color:var(${roleColorVar(role)})` : "";
    const group = slotGroups.get(String(row.slot)) || [];
    const n = Math.max(1, group.length);
    const k = Math.max(0, group.indexOf(String(item.id || "")));
    const same = n > 1 ? ` · 同槽 ${k + 1}/${n}` : "";
    const tip = `${String(item.id || "")} ${String(item.name || "")} · ${itemStatus(item)} · 槽 ${row.slot}${same}`;
    return `<i class="team-dag-cell${wrapped}" data-eff="${escapeHtml(effStatus(item))}" data-item="${escapeHtml(String(item.id || ""))}" data-slot="${row.slot}" data-same-slot="${n > 1 ? "true" : "false"}" style="--i:${row.slot};--n:${n};--k:${k}${skin}" title="${escapeHtml(tip)}"></i>`;
  }).join("");
  // 屏障没放行 = 虚线框 + 降透明度 + 🔒 待解锁（口径 5）——三样一起写，少一样都读不出"进不去"。
  const lock = frame.locked
    ? '<span class="team-dag-lock" title="屏障未放行：depends_on 里还有没 done 的里程碑（现在进不去）">🔒 待解锁</span>'
    : `<span class="team-dag-lock is-open">${status === "done" ? "✅ 已完成" : "🔓 已解锁"}</span>`;
  // 屏障 deps 是这一行**唯一**的几何外提示：它回答"这一块在等谁"。指向不存在的里程碑单独
  // 报出来（那是写错了 id，与"还没 done"是两回事）。
  const deps = frame.depends_on.length
    ? `<span class="team-dag-ms-deps" title="屏障：这些里程碑全 done 才放行">等 ${frame.depends_on.map(dep => escapeHtml(dep)).join(" + ")}</span>`
    : "";
  const missing = frame.missing_deps.length
    ? `<span class="team-dag-warn" title="depends_on 指向计划里不存在的里程碑">依赖缺失：${escapeHtml(frame.missing_deps.join("、"))}</span>`
    : "";
  const cyclic = frame.cyclic
    ? '<span class="team-dag-warn" title="depends_on 成环，层号归 0 并接在末尾">依赖成环</span>'
    : "";
  const content = frame.content
    ? `<span class="team-dag-ms-summary" title="${escapeHtml(frame.content)}">${escapeHtml(truncate(frame.content, CONTENT_LIMIT))}</span>`
    : "";
  const label = `${escapeHtml(frame.id)}${frame.name ? ` · ${escapeHtml(frame.name)}` : ""}`;
  return `<section class="team-dag-ms" data-milestone-id="${escapeHtml(frame.id)}" data-status="${escapeHtml(status)}" data-eff="${escapeHtml(eff)}" data-locked="${frame.locked ? "true" : "false"}" data-layer="${escapeHtml(String(frame.layer))}" data-sum-start="${sum.s}" data-sum-end="${sum.e}" data-empty="${sum.empty ? "true" : "false"}" data-items="${frame.rows.length}" style="--team-dag-ms-tone:var(--team-dag-ms-tone-${frame.tone})">
        <div class="team-dag-ms-label">
          <span class="team-dag-ms-line">
            <button type="button" class="team-dag-ms-open is-openable" data-team-ms-open="${escapeHtml(frame.id)}" data-tip="打开里程碑详情页（基本信息 / Work Item / 成员 / 依赖 / 动态）">${label}</button>
            <span class="team-status is-${escapeHtml(eff)}" data-eff="${escapeHtml(eff)}" title="里程碑状态：${escapeHtml(status)}">${escapeHtml(status)}</span>
            <span class="team-dag-ms-count" title="名下工作项：${frame.done} 已完成 / 共 ${frame.total}">${frame.done}/${frame.total}</span>
            ${lock}
          </span>
          <span class="team-dag-ms-wis">${frame.rows.length ? wis : '<span class="muted">尚未排活</span>'}</span>
          <span class="team-dag-ms-meta">${deps}${missing}${cyclic}${content}</span>
        </div>
        <div class="team-dag-ms-plot" style="--s:${sum.s};--e:${sum.e}">
          <span class="team-dag-ms-span" data-empty="${sum.empty ? "true" : "false"}" title="跨度：槽 ${sum.s}–${sum.e}${sum.empty ? " · 空" : ""}"></span>
          ${cells}
        </div>
      </section>`;
}

// ── 详情页（里程碑 / Work Item / teammate 三种，共用一套壳）───────────────
//
// 壳的形状：一行页签 + 一层返回。**返回栈归 app.js 管**（它才知道用户从哪儿点进来的），
// 渲染件只认两件事：当前页 ref、以及"上一层是什么"（画返回键上的字）。
//
// 为什么要页签而不是把字段堆成一长条：三种页的字段数量差一个量级（里程碑 5 个字段 +
// 一张表 + 两条反向依赖），堆在一起就是一条读不完的流水账；页签把"用户此刻要回答的那个
// 问题"隔离出来（这块是什么 / 它依赖谁 / 谁在做 / 做到哪了）。

// parseTeamPageRef 解析页 ref（"milestone:M1" / "item:WI-3" / "teammate:impl"）。
// 认不出来就返回 null —— 调用方据此不打开（不猜一个默认页出来）。
export function parseTeamPageRef(ref) {
  const text = String(ref || "");
  const at = text.indexOf(":");
  if (at <= 0) return null;
  const kind = text.slice(0, at);
  const id = text.slice(at + 1).trim();
  if (!id) return null;
  if (kind !== "milestone" && kind !== "item" && kind !== "teammate") return null;
  return { kind, id, ref: `${kind}:${id}` };
}

// teamPageRef 拼页 ref（与 parseTeamPageRef 互逆，两处只有一个写法）。
export function teamPageRef(kind, id) {
  return `${String(kind || "")}:${String(id || "")}`;
}

// teamPageTitle 是详情页标题（弹窗头 + 页内头共用**同一句话**）。
export function teamPageTitle(input = {}) {
  const ref = parseTeamPageRef(input.ref);
  if (!ref) return "团队详情";
  const plan = input.plan || null;
  if (ref.kind === "milestone") {
    const milestone = milestonesOf(plan).find(item => String(item.id) === ref.id);
    const name = String(milestone?.name || "").trim();
    return name ? `${ref.id} · ${name}` : ref.id;
  }
  if (ref.kind === "item") {
    const item = workItemsOf(plan).find(entry => String(entry.id) === ref.id);
    const name = String(item?.name || "").trim();
    return name ? `${ref.id} · ${name}` : ref.id;
  }
  return ref.id;
}

// renderTeamPage 是详情页的**唯一入口**：三种页在这里分发，app.js 只搬数据、不分支。
//
// input:
//   plan / jobs / events  同 renderTeamBoard 的三份只读投影；
//   ref                   页 ref（parseTeamPageRef 认的三种之一）；
//   tab                   当前页签 key（缺省第一页；刷新时由调用方原样带回，不把用户
//                         翻到一半的页签拽回第一页）；
//   live                  Work Item 页「执行会话」页签的实时执行面读数（拿不到就是 null）；
//   liveLoading           true = 正在读那一段正文（页面显示"读取中"，而不是把"还没读到"
//                         说成"执行面不在本进程"——那两句话是两件事）；
//   parent                上一层页（画返回键；没有就不画）。
export function renderTeamPage(input = {}) {
  const ref = parseTeamPageRef(input.ref);
  if (!ref) return '<div class="team-page is-empty">这一页认不出来（ref 必须是 milestone:… / item:… / teammate:…）</div>';
  const args = { ...input, spec: ref };
  if (ref.kind === "milestone") return renderMilestoneDetail(args);
  if (ref.kind === "item") return renderWorkItemDetail(args);
  return renderTeammateDetail(args);
}

// teamPageShell 是详情页的公共壳：页签行 + 返回键 + 逐页签一块面板。
// 面板**全部渲染出来**（不是按需再算）：切页签只是 DOM 上加一个类，没有第二次计算、
// 也没有"切回去要重算"的路径 —— 纯渲染件的幂等性因此不会被切页签这类交互破坏。
function teamPageShell({ ref, head, tabs, active, parent }) {
  const first = tabs[0]?.key || "";
  const current = tabs.some(tab => tab.key === active) ? active : first;
  const bar = tabs.map(tab =>
    `<button type="button" role="tab" class="team-page-tab${tab.key === current ? " is-active" : ""}" data-team-tab="${escapeHtml(tab.key)}" aria-selected="${tab.key === current ? "true" : "false"}">${escapeHtml(tab.label)}</button>`
  ).join("");
  const panels = tabs.map(tab =>
    `<section class="team-page-panel${tab.key === current ? " is-active" : ""}" data-team-panel="${escapeHtml(tab.key)}" role="tabpanel">${tab.html}</section>`
  ).join("");
  const back = parent?.ref
    ? `<button type="button" class="team-page-back is-openable" data-team-page-back="${escapeHtml(parent.ref)}" data-tip="返回 ${escapeHtml(parent.label || parent.ref)}">← ${escapeHtml(parent.label || parent.ref)}</button>`
    : '<span class="team-page-root">团队看板</span>';
  return `<div class="team-page" data-team-page="${escapeHtml(ref)}">
      <div class="team-page-head">${head}</div>
      <div class="team-page-bar">
        ${back}
        <div class="team-page-tabs" role="tablist">${bar}</div>
      </div>
      <div class="team-page-panels">${panels}</div>
    </div>`;
}

// teamKV 渲染一列 k→v：空值写 "—"，**不写空字符串**（空单元格读起来像"这一项没加载"，
// 与"这一项确实没有"是两回事）。value 允许是已经安全的 HTML（链接行），所以走 raw 通道时
// 由调用方自己 escape —— 本件里只有 teamLink 走这条路。
function teamKV(entries) {
  const rows = entries.map(([key, value, raw]) => {
    const text = String(value ?? "").trim();
    const body = raw ? (text || "—") : escapeHtml(text || "—");
    return `<div class="team-kv-row"><span class="team-kv-key">${escapeHtml(key)}</span><span class="team-kv-value" title="${raw ? "" : escapeHtml(text)}">${body}</span></div>`;
  }).join("");
  return `<div class="team-kv">${rows}</div>`;
}

// teamLink 是详情页里的**下钻入口**（工作项 / 里程碑 / teammate 三种页互相点得到）。
// 与看板行的入口同形（is-openable + data-tip），因为对用户来说是同一件事：这一行可以点。
function teamLink(kind, id, text) {
  const value = String(id || "").trim();
  if (!value) return '<span class="muted">—</span>';
  return `<button type="button" class="team-link is-openable" data-team-page-open="${escapeHtml(teamPageRef(kind, value))}" data-tip="打开 ${escapeHtml(text || value)} 的详情页">${escapeHtml(text || value)}</button>`;
}

// teamStatusChip 是状态 chip 的统一写法（文本写**库里那个字**，颜色由判据给）。
function teamStatusChip(eff, text, title = "") {
  return `<span class="team-status is-${escapeHtml(eff)}" data-eff="${escapeHtml(eff)}"${title ? ` title="${escapeHtml(title)}"` : ""}>${escapeHtml(text)}</span>`;
}

// teamChipRow 是 chips 行（依赖 / 成员 / 文件名这类"一个个小块"的容器）。
function teamChipRow(label, chips, empty = "—") {
  return `<div class="team-chip-row"><span class="team-label">${escapeHtml(label)}</span>${chips || `<span class="muted">${escapeHtml(empty)}</span>`}</div>`;
}

// depChip 渲染一条依赖：**带上那一头现在的状态**。只写 id 等于把"这一条放行了没"留给读的人
// 自己去计划里翻——依赖页签存在的全部理由就是回答这一句。
function depChip(plan, id, { cross = false } = {}) {
  const value = String(id || "").trim();
  if (!value) return "";
  const item = workItemsOf(plan).find(entry => String(entry.id) === value);
  const milestone = milestonesOf(plan).find(entry => String(entry.id) === value);
  const owner = item || milestone;
  const eff = item ? effStatus(item) : milestone ? milestoneEff(milestone) : "failed";
  const state = owner ? `${item ? itemStatus(item) : milestoneStatus(milestone)}${cross ? " · 跨里程碑" : ""}` : "计划里没有这一项";
  const tip = owner ? `${value} · ${state}` : `${value}：计划里没有这一项（写错了 id）`;
  return `<span class="chip team-dep" data-eff="${escapeHtml(eff)}" title="${escapeHtml(tip)}">${escapeHtml(value)} · ${escapeHtml(state)}</span>`;
}

// workItemTable 是「工作项表」的统一写法（里程碑页 / teammate 页共用）：
// ID | 名称 | 负责人 | 状态（里程碑页）或 所属里程碑（teammate 页）。
// 行里的 ID 与名称都是**下钻入口**（口径 4：两处点进同一个 Work Item 详情页）。
function workItemTable(plan, items, { milestoneColumn = false, roleColumn = false } = {}) {
  if (!items.length) return '<p class="team-page-note">名下还没有工作项。</p>';
  const head = `<tr><th>ID</th><th>名称</th>${milestoneColumn ? "<th>所属里程碑</th>" : ""}${roleColumn ? "<th>负责人</th>" : ""}<th>依赖</th><th>状态</th></tr>`;
  const rows = items.map(item => {
    const id = String(item.id || "");
    const deps = itemDepsOf(item);
    const depsHTML = deps.length
      ? deps.map(dep => `<span class="chip team-dep">${escapeHtml(dep)}</span>`).join("")
      : '<span class="muted">—</span>';
    return `<tr data-item-id="${escapeHtml(id)}">
        <td>${teamLink("item", id)}</td>
        <td>${escapeHtml(String(item.name || ""))}</td>
        ${milestoneColumn ? `<td>${teamLink("milestone", item.milestone, String(item.milestone || ""))}</td>` : ""}
        ${roleColumn ? `<td>${teamLink("teammate", item.role, String(item.role || ""))}</td>` : ""}
        <td>${depsHTML}</td>
        <td>${teamStatusChip(effStatus(item), itemStatus(item))}</td>
      </tr>`;
  }).join("");
  return `<table class="team-table"><thead>${head}</thead><tbody>${rows}</tbody></table>`;
}

// auditRows 渲染审计流水（详情页的「动态」页签）：不复用侧边栏那一节——那边是有界的
// "最近几条"，这边要的是**这一页自己的**全部相关行（按里程碑 / 工作项 / teammate 过滤）。
function auditRows(events, match, empty = "还没有与这一页相关的审计行。") {
  const list = (Array.isArray(events) ? events : []).filter(event => event && event.kind).filter(match);
  if (!list.length) return `<p class="team-page-note">${escapeHtml(empty)}</p>`;
  const rows = list.slice().reverse().map(event => {
    const target = [event.milestone, event.work_item, event.role, event.handle].filter(Boolean).map(String).join(" · ");
    const detail = String(event.detail || "").replace(/\s+/g, " ").trim();
    return `<li class="team-event" data-kind="${escapeHtml(String(event.kind || ""))}">
        <span class="chip team-event-kind is-${escapeHtml(String(event.kind || ""))}">${escapeHtml(String(event.kind || ""))}</span>
        <span class="team-event-target">${escapeHtml(target || "—")}</span>
        <time class="team-event-at">${escapeHtml(formatEventTime(event.at))}</time>
        ${detail ? `<span class="team-event-detail" title="${escapeHtml(detail)}">${escapeHtml(detail)}</span>` : ""}
      </li>`;
  }).join("");
  return `<ul class="team-events">${rows}</ul>`;
}

// jobRows 渲染某个节点（工作项 id / 里程碑 id）的作业行。
//
// 句柄自带一个不响的警报（jobs I-4）：句柄活在内存，重启后计划里残留的句柄在作业表里查不到。
// 这里只陈述**投影里有的行**，不替后端判定"它死了"——那是 jobs_manage 的事。
function jobRows(jobs, node) {
  const key = String(node || "");
  const list = (Array.isArray(jobs) ? jobs : []).filter(job => String(job?.node || "") === key);
  if (!list.length) return `<p class="team-page-note">${escapeHtml(key)} 名下没有作业行（作业句柄活在内存，重启后过期的句柄不会出现在这里）。</p>`;
  const rows = list.map(job => {
    const state = jobState(job) || "unknown";
    const eff = jobFailed(job) ? "failed" : state === "running" ? "running" : state === "done" ? "done" : "pending";
    return `<tr>
        <td><code class="team-dag-id">${escapeHtml(String(job.handle || ""))}</code></td>
        <td>${teamStatusChip(eff, state)}</td>
        <td>${escapeHtml(String(job.bytes ?? 0))} B</td>
        <td>${escapeHtml(String(job.exit_code ?? 0))}</td>
        <td>${escapeHtml(String(job.role || job.scope?.subject || ""))}</td>
      </tr>`;
  }).join("");
  return `<table class="team-table"><thead><tr><th>句柄</th><th>状态</th><th>字节</th><th>退出码</th><th>归属</th></tr></thead><tbody>${rows}</tbody></table>`;
}

// renderMilestoneDetail 渲染**里程碑详情页**：基本信息 / Work Item(n) / 成员 / 依赖 / 动态。
export function renderMilestoneDetail(input = {}) {
  const plan = input.plan || null;
  const id = input.spec?.id || "";
  const events = Array.isArray(input.events) ? input.events : [];
  const jobs = Array.isArray(input.jobs) ? input.jobs : [];
  const frame = ganttModel(plan).frames.find(item => item.id === id);
  if (!frame) return `<div class="team-page is-empty">里程碑 ${escapeHtml(id)} 已不在计划里（可能已收口）。</div>`;
  const eff = milestoneEff(frame);
  const status = frame.status;
  const items = frame.rows.map(row => row.item);
  const reverse = milestonesOf(plan)
    .filter(milestone => milestoneDepsOf(milestone).includes(id))
    .map(milestone => String(milestone.id));
  const head = `<span class="team-page-kind">里程碑</span>
      <code class="team-dag-id">${escapeHtml(frame.id)}</code>
      ${frame.name ? `<h3 class="team-page-name">${escapeHtml(frame.name)}</h3>` : ""}
      ${teamStatusChip(eff, status, "里程碑状态（sessionstore：pending / active / done）")}
      <span class="chip team-count" title="名下工作项：${frame.done} 已完成 / 共 ${frame.total}">${frame.done}/${frame.total}</span>
      ${frame.locked ? '<span class="team-dag-lock">🔒 待解锁</span>' : '<span class="team-dag-lock is-open">🔓 已解锁</span>'}`;
  const wis = frame.rows.map(row => {
    const item = row.item;
    const role = String(item.role || "").trim();
    const skin = role ? ` style="--team-dag-role-color:var(${roleColorVar(role)})"` : "";
    const tip = `${String(item.id || "")} ${String(item.name || "")} · ${itemStatus(item)} · 槽 ${row.slot}${role ? " · @" + role : ""}`;
    return `<span class="chip team-wi" data-eff="${escapeHtml(effStatus(item))}" data-item="${escapeHtml(String(item.id || ""))}"${skin} title="${escapeHtml(tip)}"><i class="team-dag-dot"></i>${escapeHtml(String(item.id || ""))}</span>`;
  }).join("");
  const barriers = frame.depends_on.length ? frame.depends_on.map(dep => depChip(plan, dep)).join("") : "";
  const after = frame.after.length ? frame.after.map(dep => depChip(plan, dep)).join("") : "";
  const reverseChips = reverse.length ? reverse.map(dep => depChip(plan, dep)).join("") : "";
  const members = [...new Set(items.map(item => String(item.role || "").trim()).filter(Boolean))];
  const memberChips = members.map(role => {
    const owned = memberWorkOf(plan, role).filter(item => String(item.milestone || "") === id);
    const done = owned.filter(item => itemStatus(item) === "done").length;
    return `<span class="chip team-member-chip" style="--team-dag-role-color:var(${roleColorVar(role)})"><i class="team-dag-dot"></i>${teamLink("teammate", role, role)}<span class="muted">${done}/${owned.length}</span></span>`;
  }).join("");
  // 里程碑内的依赖边：源 → 目标，一行一条。这就是"不画箭头，写下来"的那个写法。
  const pairByID = new Map(items.map(item => [String(item.id), item]));
  const edges = [];
  for (const item of items) {
    for (const dep of itemDepsOf(item)) {
      if (!pairByID.has(dep)) continue;
      edges.push(`${dep} → ${String(item.id)}`);
    }
  }
  const edgeRows = edges.length
    ? `<ul class="team-dep-list">${edges.map(edge => `<li><code class="team-dag-id">${escapeHtml(edge)}</code></li>`).join("")}</ul>`
    : '<p class="team-page-note">这块里程碑内的工作项之间没有依赖（同槽并行）。</p>';
  const tabs = [
    {
      key: "basic",
      label: "基本信息",
      html: teamKV([
        ["里程碑名称", `${frame.id}${frame.name ? " · " + frame.name : ""}`],
        ["状态", status],
        ["计划进度", `${frame.done}/${frame.total} 已完成`],
        ["依赖槽位", `槽 ${frame.sum.s}–${frame.sum.e}（1 格 = 1 层依赖；**不是工期**）`],
        ["屏障层号", `L${Number(frame.depth) || 0}`],
        ["屏障", frame.depends_on.length ? frame.depends_on.join(" + ") : ""],
        ["判据", frame.after.length ? frame.after.join(" + ") : ""],
        ["工作项编号", frame.rows.map(row => String(row.item.id || "")).join(" ")],
        ["内容", frame.content],
      ]) + `<div class="team-chip-row">${wis || '<span class="muted">尚未排活</span>'}</div>`,
    },
    { key: "items", label: `Work Item(${items.length})`, html: workItemTable(plan, items, { roleColumn: true }) },
    { key: "members", label: `成员(${members.length})`, html: teamChipRow("执行 teammate", memberChips, "这一块还没有排活") },
    {
      key: "deps",
      label: "依赖",
      html: `${teamChipRow("屏障（这些里程碑全 done 才放行）", barriers, "无屏障：这一块不挡在任何里程碑后面")}
        ${teamChipRow("判据 after", after, "没有声明判据")}
        ${teamChipRow("被依赖（谁在等它）", reverseChips, "没有别的里程碑在等它")}
        <div class="team-page-sub">里程碑内的工作项依赖（源 → 目标）</div>${edgeRows}
        <div class="team-page-sub">名下工作项的作业行</div>${jobRows(jobs, frame.id)}`,
    },
    {
      key: "events",
      label: "动态",
      html: auditRows(events, event => String(event.milestone || "") === frame.id || frame.rows.some(row => String(event.work_item || "") === row.id),
        "还没有与这块里程碑相关的审计行。"),
    },
  ];
  return teamPageShell({ ref: teamPageRef("milestone", frame.id), head, tabs, active: input.tab, parent: input.parent });
}

// renderWorkItemDetail 渲染**Work Item 详情页**。
//
// 这一页是**共用件**（口径 3）：里程碑页的「Work Item」页签与 teammate 页的「负责的 Work Item」
// 页签都点到这里来。所以它只认 plan + 这件事的 id，不认"从哪儿来"——从哪儿来只影响返回键上的
// 字（input.parent），不影响页里任何一条读数。
export function renderWorkItemDetail(input = {}) {
  const plan = input.plan || null;
  const id = input.spec?.id || "";
  const events = Array.isArray(input.events) ? input.events : [];
  const jobs = Array.isArray(input.jobs) ? input.jobs : [];
  const item = workItemsOf(plan).find(entry => String(entry.id) === id);
  if (!item) return `<div class="team-page is-empty">工作项 ${escapeHtml(id)} 已不在计划里（可能已收口）。</div>`;
  const status = itemStatus(item);
  const eff = effStatus(item);
  const role = String(item.role || "").trim();
  const milestoneID = String(item.milestone || "");
  const deps = itemDepsOf(item);
  const allByID = new Map(workItemsOf(plan).map(entry => [String(entry.id), entry]));
  const missing = deps.filter(dep => !allByID.has(dep));
  const cross = deps.filter(dep => allByID.has(dep) && String(allByID.get(dep).milestone || "") !== milestoneID);
  const blocked = deps.some(dep => itemStatus(allByID.get(dep)) !== "done");
  const reverse = workItemsOf(plan).filter(entry => itemDepsOf(entry).includes(id)).map(entry => String(entry.id || ""));
  const slot = slotOf(id, allByID);
  const session = String(item.session_id || "").trim();
  const head = `<span class="team-page-kind">Work Item</span>
      <code class="team-dag-id">${escapeHtml(id)}</code>
      ${item.name ? `<h3 class="team-page-name">${escapeHtml(item.name)}</h3>` : ""}
      ${teamStatusChip(eff, status, "工作项状态（空 = pending）")}
      ${item.interrupted === true ? '<span class="chip team-interrupted" title="状态说在跑、而本进程的作业表里查不到它的句柄（jobs I-4）。可以重派——会话与现场都还在">可重派</span>' : ""}
      ${item.live === true ? '<span class="chip team-live" title="这件事现在真的有一份未释放的工作区绑定">现场在</span>' : ""}
      ${blocked ? '<span class="chip team-blocked" title="前置工作项还没验收通过：现在派发会被闸门拒">被依赖卡住</span>' : ""}`;
  const depChips = deps.length ? deps.map(dep => depChip(plan, dep, { cross: cross.includes(dep) })).join("") : "";
  const reverseChips = reverse.length ? reverse.map(dep => depChip(plan, dep)).join("") : "";
  const messages = memberMessagesOf(plan, role)
    .filter(message => String(message.work_item || "") === id)
    .map(message => `<li class="team-item-message"><span class="team-event-at">${escapeHtml(formatEventTime(message.at || ""))}</span><span>${escapeHtml(String(message.text || ""))}</span></li>`)
    .join("");
  const sessionPanel = !session
    ? `<p class="team-page-note">这件事还没有自己的会话（未派发 / 已销项）。${escapeHtml(noOwnSessionNote(role || "这一位"))}</p>`
    : input.liveLoading === true
      ? '<p class="team-page-note">正在读这一轮的执行面…（正文不落盘，只能从执行面实时读）</p>'
      : renderTeammateLiveSession(input.live, { role, work_item: id });
  const tabs = [
    {
      key: "basic",
      label: "基本信息",
      html: teamKV([
        // 两格是**下钻入口**（raw=true：值本身是 teamLink 出来的安全 HTML），所以它们不走
        // escape 通道——本件里只有这一种值走 raw，且都由 teamLink / teamStatusChip 构造。
        ["所属里程碑", teamLink("milestone", milestoneID, milestoneID), true],
        ["负责人", teamLink("teammate", role, role ? "@" + role : ""), true],
        ["状态", status],
        ["依赖槽位", `槽 ${slot}（1 格 = 1 层依赖；**不是工期**）`],
        ["依赖", deps.length ? deps.join(" → ") : ""],
        ["句柄", String(item.handle || "")],
        ["工作区", String(item.worktree || "")],
        ["会话", session],
        ["达成目标", String(item.goal || "")],
        ["描述", String(item.description || "")],
        ["结论", String(item.note || "")],
      ]),
    },
    {
      key: "deps",
      label: "依赖",
      html: `${teamChipRow("depends_on（这些做完才轮到它）", depChips, "无依赖：可以立刻派")}
        ${teamChipRow("被依赖（谁在等它）", reverseChips, "没有别的工作项在等它")}
        ${missing.length ? `<p class="team-page-warn">依赖缺失：${escapeHtml(missing.join("、"))}（depends_on 指向**计划里**不存在的 id）</p>` : ""}
        ${cross.length ? `<p class="team-page-warn">跨里程碑依赖：${escapeHtml(cross.join("、"))}（编排口径只允许同里程碑内 —— 派活时这条边作废）</p>` : ""}
        ${blocked ? '<p class="team-page-note">前置还没验收通过：现在派发会被闸门拒（blocked）。</p>' : ""}`,
    },
    { key: "session", label: "执行会话", html: sessionPanel },
    {
      key: "jobs",
      label: "作业与回执",
      html: `${jobRows(jobs, id)}
        ${messages ? `<div class="team-page-sub">尾插回执</div><ul class="team-item-messages">${messages}</ul>` : '<p class="team-page-note">还没有尾插回执。</p>'}`,
    },
    {
      key: "events",
      label: "动态",
      html: auditRows(events, event => String(event.work_item || "") === id || (!!role && String(event.role || "") === role && String(event.node || "") === id),
        "还没有与这件事相关的审计行。"),
    },
  ];
  return teamPageShell({ ref: teamPageRef("item", id), head, tabs, active: input.tab, parent: input.parent });
}

// renderTeammateDetail 渲染**teammate 详情页**：基本信息 / 负责的 Work Item(n) / 插件装配 / 动态。
//
// 「负责的 Work Item」页签里的每一行都点进**同一个 Work Item 详情页**（口径 3）——teammate
// 手足的多件事因此共用一套子页面，不各写一份。
export function renderTeammateDetail(input = {}) {
  const plan = input.plan || null;
  const role = String(input.spec?.id || "");
  const events = Array.isArray(input.events) ? input.events : [];
  const jobs = Array.isArray(input.jobs) ? input.jobs : [];
  if (!role) return '<div class="team-page is-empty">角色名为空。</div>';
  const member = memberOf(plan, role);
  const status = memberStatusOf(plan, role);
  const owned = memberWorkOf(plan, role);
  const done = owned.filter(item => itemStatus(item) === "done").length;
  const current = memberCurrentSessionOf(plan, role);
  const entry = teammateSessionEntry(plan, role);
  const policy = String(member?.tools_policy || "").trim();
  const worktree = String(member?.worktree || "").trim();
  const live = entry.kind === "live"
    ? `<button type="button" class="chip team-member-session is-openable" data-team-role-open="${escapeHtml(role)}" data-team-role-session="${escapeHtml(entry.session_id)}" data-team-item="${escapeHtml(entry.work_item || "")}" data-tip="打开 ${escapeHtml(role)} 此刻那件事的会话（工作项 ${escapeHtml(entry.work_item || "—")}）">当前会话</button>`
    : `<span class="team-page-note">${escapeHtml(String(entry.reason || "这一位此刻没有自己的会话"))}</span>`;
  const head = `<span class="team-page-kind">teammate</span>
      <code class="team-dag-id" style="--team-dag-role-color:var(${roleColorVar(role)})">${escapeHtml(role)}</code>
      ${teamStatusChip(status, status, "这位此刻在不在干活（只有 running / free 两个值）")}
      ${policy ? `<span class="chip team-policy is-${escapeHtml(policy)}">${escapeHtml(policy)}</span>` : ""}
      <span class="chip team-count" title="负责 ${owned.length} 件事，已完成 ${done} 件">${done}/${owned.length}</span>
      ${member ? "" : '<span class="chip team-stale" title="这位不在当前计划的 members[] 里（可能是别的团队的角色）">不在编</span>'}`;
  const queue = memberQueueOf(plan, role);
  const queueChips = queue.length ? queue.map(name => `<span class="chip team-queue-item">${escapeHtml(name)}</span>`).join("") : "";
  const messages = memberMessagesOf(plan, role)
    .map(message => `<li class="team-item-message"><span class="team-event-at">${escapeHtml(formatEventTime(message.at || ""))}</span><span>${escapeHtml(String(message.text || ""))}</span></li>`)
    .join("");
  // 「装配」页签后半段列这位名下**每一件事**的作业行：装配是 teammate 级的，"它对哪些会话
  // 生效"就是这一串工作项——装配页与作业行放同一页，读的人不必自己把两页对起来。
  // null 表示"名下确实一件都没开过工"，与"有工作项但没有作业行"（jobRows 自己会说）分开。
  const jobBlocks = owned
    .map(item => ({ item, jobs: (Array.isArray(jobs) ? jobs : []).filter(job => String(job?.node || "") === String(item.id || "")) }))
    .filter(entry2 => entry2.jobs.length);
  const ownedWithJobs = jobBlocks.length
    ? jobBlocks.map(entry2 => `<div class="team-page-sub">${escapeHtml(String(entry2.item.id || ""))} · ${escapeHtml(String(entry2.item.name || ""))}</div>${jobRows(jobs, entry2.item.id)}`).join("")
    : '<p class="team-page-note">名下工作项还没有作业行（句柄活在内存，重启后过期的句柄不会出现在这里）。</p>';
  const tabs = [
    {
      key: "basic",
      label: "基本信息",
      html: teamKV([
        ["角色", role],
        ["状态", status],
        ["权责", policy],
        ["工作区", worktree],
        ["长期角色会话", String(member?.role_session_id || "")],
        ["此刻那件事", current.work_item ? `${current.work_item}${current.name ? " · " + current.name : ""}` : ""],
        ["此刻那件事的会话", current.session_id],
        ["负责", `${owned.length} 件事（已完成 ${done} 件）`],
      ]) + `<div class="team-chip-row"><span class="team-label">入口</span>${live}</div>`,
    },
    { key: "items", label: `负责的 Work Item(${owned.length})`, html: workItemTable(plan, owned, { milestoneColumn: true }) },
    {
      key: "assembly",
      label: "插件装配",
      html: (renderMemberAssembly(member) || '<p class="team-page-note">这一位既没有声明面，也没有生效读数（空集 = 不覆盖由 mode=inherit-host 回答；读数为空 = 桥这一侧给不出）。</p>')
        + `<p class="team-page-note">装配是 <b>teammate 级</b>：一位 teammate = 一条长期角色会话 + 每个工作项自己的会话，这份装配对它手上的每个工作项会话都生效。</p>`
        + `<div class="team-page-sub">名下工作项的作业行</div>`
        + ownedWithJobs,
    },
    {
      key: "events",
      label: "动态",
      html: (queueChips ? `<div class="team-chip-row"><span class="team-label">未完成队列</span>${queueChips}</div>` : "")
        + auditRows(events, event => String(event.role || "") === role, "还没有与这一位相关的审计行。")
        + (messages ? `<div class="team-page-sub">尾插回执</div><ul class="team-item-messages">${messages}</ul>` : ""),
    },
  ];
  return teamPageShell({ ref: teamPageRef("teammate", role), head, tabs, active: input.tab, parent: input.parent });
}

// ── 渲染 ────────────────────────────────────────────────────────

// renderTeamBoard 渲染整块看板：看板头 + 里程碑格栅 + teammate 条目 + 审计。
//
// 没有里程碑也没有工作项 → ""（不留空壳，口径同目标看板）。判据只看里程碑与工作项：
// 阶段口径已删除，不存在"计划的形状里有阶段"这回事。
export function renderTeamBoard(input = {}) {
  const { plan = null, jobs = [], events = [], maxMembers = 0, stale = false, recovered = false } = input;
  const milestones = milestonesOf(plan);
  const items = workItemsOf(plan);
  if (milestones.length === 0 && items.length === 0) return "";
  const summary = summarizeTeam(plan, jobs);
  return `<div class="team-board" data-team-board data-team-id="${escapeHtml(String(plan?.team_id || ""))}">
      ${renderTeamHead(plan, summary, maxMembers, stale, recovered)}
      ${renderTeamGantt(plan)}
      ${renderTeamQueue(plan)}
      ${renderTeamAudit(events)}
    </div>`;
}

// renderTeamHead 是看板头：team_id + 版本 + 一行计数。计数只报事实（不写「进度良好」这类话）。
//
// 两个标记都是**痕迹**，不是装饰：
//   - recovered：这份看板来自存档快照（活体投影给不出时兜底），不是活体算出来的；
//   - stale：里面的作业行句柄来自上一个进程（jobs I-4），真值以 Observe 为准。
// 恢复出来的看板两者同时为真，所以同屏出现（不是二选一）。
export function renderTeamHead(plan, summary, maxMembers = 0, stale = false, recovered = false) {
  const version = Number(plan?.version) || 0;
  const teamID = String(plan?.team_id || "未命名团队");
  const roster = maxMembers > 0 ? `在编 ${summary.members}/${maxMembers}` : `在编 ${summary.members}`;
  const counts = [
    `工作项 ${summary.items_done}/${summary.items_total} 完成`,
    summary.items_running ? `${summary.items_running} 在跑` : "",
    summary.items_review ? `${summary.items_review} 待验收` : "",
    summary.items_failed ? `${summary.items_failed} 未通过` : "",
    roster,
    `作业 ${summary.jobs_running} 跑 / ${summary.jobs_done} 完 / ${summary.jobs_failed} 败`,
    `里程碑 ${summary.milestones_done}/${summary.milestones_total}`,
  ].filter(Boolean).join(" · ");
  const recoveredChip = recovered
    ? '<span class="chip team-recovered" title="这块看板来自会话存档快照（活体投影给不出时才兜底），活体一恢复就会覆盖回活体结果">自快照恢复</span>'
    : "";
  const staleChip = stale
    ? '<span class="chip team-stale" title="jobs I-4：句柄只在内存，进程重启后的投影一律过期，真值以 Observe 为准">句柄投影可能过期</span>'
    : "";
  return `<div class="team-board-head">
      <span class="team-badge">TEAM</span>
      <span class="team-board-id" title="${escapeHtml(teamID)}">${escapeHtml(truncate(teamID, TEXT_LIMIT))}</span>
      ${version > 0 ? `<span class="team-version">v${escapeHtml(String(version))}</span>` : ""}
      ${recoveredChip}
      ${staleChip}
      <span class="team-counts">${escapeHtml(counts)}</span>
    </div>`;
}

// renderMemberAssembly 渲染一位 teammate 的**插件装配**（声明面 + 生效读数）。
//
// 数据两格（2026-10-05 · leader 冻结的契约，后端 commit 39a8b63）：
//   - member.plugins  = **声明面**（计划 members[].plugins 规整后那一份；空 = 不覆盖）；
//   - member.assembly = **生效读数**（mode / 技能目录字节 / 插件面工具数 / 黄牌 / 失灵）。
// 两格合成一件事的两个侧面：只有声明面说不清"空集 = 继承宿主"还是"装了个不存在的名字"
// （前者 inherit-host，后者 plugin_face_faulted）；只有读数说不清"这位被**要求**装什么"。
//
// **装配是 teammate 级的**：一位 teammate = 一条长期角色会话 + 每个工作项**自己的**会话，
// 声明落在这一位身上，对它手上的**每个工作项会话**都生效——所以这一行的文案要说这句，
// 而不是让读的人以为这是"某个工作项"的装配。
//
// 口径（照写，不自由发挥）：mode 的每个取值都**显式写出来**，不靠字段缺失暗示——
//   replace      → 每个插件一枚 chip + 一行读数（技能 n / 目录 xB · ≈y tok / 工具面 a/b）
//                  + 黄牌（title = yellow_reason）；黄牌**只报不拒**，不遮其它读数；
//   inherit-host → 显式写「继承宿主」（空集 = 不覆盖：工具面继承宿主 + 技能目录不注入）；
//   **assembly 为 nil → 不显示装配格**（2026-10-05 审查 P0 修正，回到契约原文）：
//                  `dto/teamwork_board.go` 的 Assembly 注释写的是"nil 表示**桥这一侧给不出
//                  读数**（未装配插件域 / 成员行不在读数里）：前端据此不显示装配格，而不是
//                  把缺失读成'0 个工具、0 份技能'"。所以这里既不许写「按不覆盖处理」（读数
//                  缺失 ≠ 空集），也不许写「继承宿主」——前者把缺失当读数，后者替后端断言了
//                  前端无从知道的语义（"空集 = 不覆盖"只能由 mode=inherit-host 回答）。
//                   有声明面时只列声明 chips（计划的事实），一个 mode 结论都不写。
// plugin_face_faulted / plugin_face_missing 是**失灵读数**：声明过的插件在本进程已无定义
// （root 撤销过 / 名字漂了）。它们与"没装配"**语义相反**，所以**绝不**渲染成继承宿主那
// 一支——Mode 仍是 replace、声明仍是那一份，本件只在此之上加「失灵 / 已撤」标记 + Note。
export function renderMemberAssembly(member) {
  const declared = (Array.isArray(member?.plugins) ? member.plugins : [])
    .map(name => String(name ?? "").trim())
    .filter(Boolean);
  const assembly = member?.assembly && typeof member.assembly === "object" ? member.assembly : null;
  const mode = String(assembly?.mode || "").trim();
  // 声明面 chips：复用本文件既有 chip 类。长插件名不许撑破行——三层一起兜（chips 容器
  // 换行 + 单枚 max-width + 文本截断），title 里留全名。
  const chips = declared.map(name =>
    `<span class="chip team-assembly-plugin" title="${escapeHtml(name)}">${escapeHtml(truncate(name, 48))}</span>`
  ).join("");
  const scope = `<span class="team-assembly-scope" title="装配声明在 teammate 级：一位 teammate = 一条长期角色会话 + 每个工作项自己的会话；这份装配对它手上的每个工作项会话都生效">teammate 级 · 对这位每个工作项会话都生效</span>`;
  // **读数给不出 / 没写明 mode ⟹ 不下 mode 结论**（契约：Assembly 为 nil = 桥这一侧给不出
  // 读数 → 前端不显示装配格）。声明面是**计划**的事实、与读数无关：有声明时就只列声明 chips
  // ——它既不否认也不证实任何 mode，所以一个字都不说「继承宿主」（"被要求装了两个插件"与
  // "继承宿主"互斥，同屏就是自相矛盾）；没有声明面就整格退场，不留空壳。
  if (!assembly || (mode !== "replace" && mode !== "inherit-host")) {
    if (declared.length === 0) return "";
    return `<div class="team-member-assembly">
        <span class="team-label">装配</span>
        ${chips}
        ${scope}
      </div>`;
  }
  // 黄牌：目录段超阈值（6k token 估算 / 上下文窗口 2%）。**只报不拒**——它是一句提示，
  // 不改变这一位能不能干活，所以和读数同屏、不替换读数。
  const yellow = assembly?.yellow === true
    ? `<span class="chip team-assembly-yellow" title="${escapeHtml(String(assembly?.yellow_reason || "技能目录超阈值（只报不拒）"))}">黄牌</span>`
    : "";
  const missing = (Array.isArray(assembly?.plugin_face_missing) ? assembly.plugin_face_missing : [])
    .map(name => String(name ?? "").trim())
    .filter(Boolean);
  const faulted = assembly?.plugin_face_faulted === true
    ? `<span class="chip team-assembly-faulted" title="${escapeHtml(String(assembly?.plugin_face_note || "声明过的插件在本进程已无定义（工具面为空）"))}">失灵</span>`
    : "";
  const withdrawn = missing.length
    ? `<span class="chip team-assembly-missing" title="${escapeHtml("已被撤销 / 名字漂移（本进程已无定义）：" + missing.join("、"))}">已撤 ${escapeHtml(String(missing.length))}</span>`
    : "";
  const note = String(assembly?.plugin_face_note || "").trim();
  let readout;
  if (mode === "replace") {
    const count = value => Number(value) || 0;
    // 窄栏里这一行会被省略号吃掉（审查 Hypothesis）：title 里放**同一句话的全文** +
    // 上界口径说明，悬停就能读到被截掉的部分，而不是让读者猜。
    const readoutText = `技能 ${count(assembly?.skill_count)} / 目录 ${count(assembly?.skill_catalog_runes)}B · ≈${count(assembly?.skill_catalog_tokens_est)} tok / 工具面 ${count(assembly?.plugin_face_tools)}/${count(assembly?.total_tools)}`;
    const readoutTip = `${readoutText}（生效读数：插件面工具数是上界——全量工具里过得了插件收窄的那些，实际可见面还要与权限面相交，只会更小）`;
    readout = `<span class="team-assembly-readout" title="${escapeHtml(readoutTip)}">${escapeHtml(readoutText)}</span>`;
  } else {
    readout = `<span class="team-assembly-inherit" title="空集 = 不覆盖：工具面继承宿主当前装配，技能目录不注入（运行事实，不是读数没算）">继承宿主</span>`;
  }
  return `<div class="team-member-assembly" data-assembly-mode="${escapeHtml(mode)}">
        <span class="team-label">装配</span>
        ${chips}
        ${readout}
        ${yellow}${faulted}${withdrawn}
        ${note ? `<span class="team-assembly-note" title="${escapeHtml(note)}">${escapeHtml(truncate(note, CONTENT_LIMIT))}</span>` : ""}
        ${scope}
      </div>`;
}

// renderTeamQueue 是**teammate 条目**：一行一位，报最小可读集——角色名（**入口**）/ 状态 /
// 负责几件事 / 工作区 / 权责。装配、队列明细、回执明细、此刻那件事的会话都在**详情页**里
// （点角色名进去），侧边栏不再逐条铺开。
//
// 两个入口各有各的钩子，别混：
//   - 角色名 → `data-team-member-open`：打开这一位的**详情页**；
//   - 「当前会话」小 chip（只在真有"这件事自己的会话"时才出现）→ `data-team-role-open` /
//     `data-team-role-session`：直接开那一轮的**实时执行面**（与 Agent Team 面板成员行同一个
//     openRoleSessionDetail，不另造"员工会话"概念）。没有自己的会话时**不挂这个 chip**
//     （纯文本 + 说清为什么），而不是回退到角色会话——一个内容错的入口比没有入口更坏。
export function renderTeamQueue(plan) {
  const members = Array.isArray(plan?.members) ? plan.members.filter(Boolean) : [];
  if (members.length === 0) return "";
  const rows = members.map(member => {
    const role = String(member.role || "");
    const policy = String(member.tools_policy || "").trim();
    const worktree = String(member.worktree || "").trim();
    const owned = memberWorkOf(plan, role);
    const done = owned.filter(item => itemStatus(item) === "done").length;
    const running = owned.filter(item => itemStatus(item) === "running").length;
    const status = memberStatusOf(plan, role);
    const entry = teammateSessionEntry(plan, role);
    const countTip = `负责 ${owned.length} 件事：已完成 ${done} 件${running ? ` · ${running} 件在跑` : ""}`;
    const sessionChip = entry.kind === "live"
      ? `<button type="button" class="chip team-member-session is-openable" data-team-role-open="${escapeHtml(role)}" data-team-role-session="${escapeHtml(entry.session_id)}" data-team-item="${escapeHtml(entry.work_item || "")}" data-tip="打开 ${escapeHtml(role)} 此刻那件事的会话（工作项 ${escapeHtml(entry.work_item || "—")}）">当前会话</button>`
      : "";
    return `<li class="team-member" data-role="${escapeHtml(role)}" data-status="${escapeHtml(status)}" style="--team-dag-role-color:var(${roleColorVar(role)})">
        <button type="button" class="team-member-name is-openable" data-team-member-open="${escapeHtml(role)}" data-tip="打开 ${escapeHtml(role)} 的详情页（基本信息 / 负责的 Work Item / 插件装配 / 动态）"><i class="team-dag-dot"></i>${escapeHtml(role)}</button>
        ${teamStatusChip(status, status, "这位此刻在不在干活（只有 running / free 两个值）")}
        <span class="team-member-count" title="${escapeHtml(countTip)}">${owned.length} 件事${done ? ` · ${done} 完成` : ""}</span>
        ${policy ? `<span class="chip team-policy is-${escapeHtml(policy)}">${escapeHtml(policy)}</span>` : ""}
        <span class="team-member-wt" title="${escapeHtml(worktree || "未指派工作区（回退主工作区）")}">${escapeHtml(worktree || "主工作区")}</span>
        ${sessionChip}
      </li>`;
  }).join("");
  return `<section class="team-section" data-team-queue>
      <div class="team-section-title"><span>teammate</span><span class="chip team-count" title="在编 ${members.length} 位">${members.length}</span><span class="team-section-hint">点名字看详情</span></div>
      <ul class="team-members">${rows}</ul>
    </section>`;
}

// renderTeammateLiveSession 渲染**当前 teammate 会话**的实时子页面（这件事自己的会话）。
//
// 为什么单开一个渲染形状而不是复用 renderRoleSessionDetail（2026-10-04 用户口径：
// 「查看 teammate 的会话，全是历史会话不是当前的 teammate 的会话」）：角色会话详情读的是
// **员工的长期角色会话**（跨工作项、跨轮次的落盘历史），而一个 Work Item 的会话是**进程内
// 执行面**——它的正文不在会话库里，只能从执行面实时读（后端 TeammateSessionLive 的那份
// 投影）。把后者硬塞进前者的形状，就会出现"看到的是历史、还以为在看当前"。
//
// 判据诚实：`running=false` 表示这一轮的执行面不在本进程（重启过 / 已收口），此时**不画
// 空壳**，直接说清"正文不落盘、看不到"——那正是用户这次报的那件事的反面。
export function renderTeammateLiveSession(view, meta = {}) {
  const session = String(view?.session_id || meta.session_id || "");
  const role = String(view?.role || meta.role || "");
  const workItem = String(meta.work_item || "");
  const chips = [
    `<span class="chip team-item-ref">${escapeHtml(workItem || "这件事")}</span>`,
    role ? `<span class="chip team-role">${escapeHtml(role)}</span>` : "",
    view?.live ? '<span class="chip team-status is-running">正在跑</span>' : '<span class="chip team-status is-free">已跑完</span>',
  ].filter(Boolean).join("");
  const head = `<div class="role-session-head">${chips}</div>`;
  if (!view?.running) {
    return `<div class="role-session-view team-item-panel" data-teammate-live="${escapeHtml(session)}">
      ${head}
      <div class="team-item-note">这一轮的执行面不在本进程（可能重启过或已收口）。teammate 的一轮活是**进程内**执行面，正文不落盘——这里读不到，不是"会话是空的"。</div>
      <div class="team-item-note">结论与回执走团队看板：尾插回执在 teammate 那一行，产物正文在 team_context / jobs_manage。</div>
    </div>`;
  }
  const rows = (Array.isArray(view?.messages) ? view.messages : [])
    // 只画输入与回答两类：工具行的原始载荷是执行细节（后端已经筛过一遍，这里再筛一次是
    // 纯函数该有的自觉——渲染件不画它不理解的东西）。
    .filter(message => message && (String(message.role || "") === "user" || String(message.role || "") === "assistant"))
    .filter(message => String(message.text || "").trim())
    .map(message => {
      const who = String(message.role || "") === "user" ? "输入" : (role || "teammate");
      const text = String(message.text || "").trim();
      return `<div class="role-kv-row team-live-row is-${escapeHtml(String(message.role || ""))}"><span class="role-kv-key">${escapeHtml(who)}</span><span class="role-kv-value" title="${escapeHtml(text)}">${escapeHtml(text)}</span></div>`;
    }).join("");
  const truncated = view?.truncated
    ? '<div class="team-item-note">（只显示最近若干条；单条过长已截断）</div>'
    : "";
  return `<div class="role-session-view team-item-panel" data-teammate-live="${escapeHtml(session)}">
      ${head}
      <div class="role-kv">${rows || '<div class="team-item-note">这一轮还没有对话（刚派发，或回合还没写第一行）。</div>'}</div>
      ${truncated}
      ${teammateLivePager(view)}
    </div>`;
}

// TEAMMATE_LIVE_PAGE_SIZE 是翻页时前端申请的页大小（与后端 limit<=0 的默认页一致）。
export const TEAMMATE_LIVE_PAGE_SIZE = 40;

// teammateLivePager 画「这件事的会话」的翻页条。
//
// 后端读数已经是**有界窗口 + 分页**（offset/limit/total/has_more）：前端只搬这些读数，
// 不自己推算事实——offset 是后端归一过的起点，has_more 是后端算的"后面还有"。
// 读不到这些键（老载荷/夹具）时退化成"只有这一页"：不画假的翻页键。
function teammateLivePager(view) {
  const messages = Array.isArray(view?.messages) ? view.messages : [];
  const total = Number.isFinite(Number(view?.total)) ? Number(view.total) : messages.length;
  if (total <= 0) return "";
  const offset = Number.isFinite(Number(view?.offset)) && Number(view.offset) > 0 ? Number(view.offset) : 0;
  const limit = Number.isFinite(Number(view?.limit)) && Number(view.limit) > 0 ? Number(view.limit) : TEAMMATE_LIVE_PAGE_SIZE;
  const hasMore = view?.has_more === true;
  const first = offset + 1;
  const last = offset + messages.length;
  const prevOffset = Math.max(0, offset - limit);
  const prev = offset > 0
    ? `<button class="chip team-live-page" data-teammate-live-page="${prevOffset}">上一页</button>`
    : '<button class="chip team-live-page" data-teammate-live-page="0" disabled>上一页</button>';
  const next = hasMore
    ? `<button class="chip team-live-page" data-teammate-live-page="${offset + limit}">下一页</button>`
    : `<button class="chip team-live-page" data-teammate-live-page="${offset + limit}" disabled>下一页</button>`;
  return `<div class="team-live-pager">
      ${prev}
      <span class="team-live-range">第 ${first}–${last} 条 / 共 ${total} 条</span>
      ${next}
    </div>`;
}

// renderTeamAudit 是审计流水的**尾巴**（只追加、不重写；倒序 = 最新的在最上面）。
// 全量不在侧边栏：详情页的「动态」页签按里程碑 / 工作项 / teammate 各自过滤后再读。
export function renderTeamAudit(events) {
  const list = (Array.isArray(events) ? events : []).filter(item => item && item.kind);
  if (list.length === 0) return "";
  const recent = list.slice(-AUDIT_LIMIT).reverse();
  const rows = recent.map(event => {
    const kind = String(event.kind || "");
    const target = [event.milestone, event.work_item, event.role, event.handle].filter(Boolean).map(String).join(" · ");
    const detail = String(event.detail || "").replace(/\s+/g, " ").trim();
    return `<li class="team-event" data-kind="${escapeHtml(kind)}">
        <span class="chip team-event-kind is-${escapeHtml(kind)}">${escapeHtml(kind)}</span>
        <span class="team-event-target">${escapeHtml(target || "—")}</span>
        <time class="team-event-at">${escapeHtml(formatEventTime(event.at))}</time>
        ${detail ? `<span class="team-event-detail" title="${escapeHtml(detail)}">${escapeHtml(truncate(detail, TEXT_LIMIT))}</span>` : ""}
      </li>`;
  }).join("");
  return `<section class="team-section" data-team-audit>
      <div class="team-section-title"><span>审计</span><span class="chip team-count" title="共 ${list.length} 条，这里显示最近 ${recent.length} 条；全量看各详情页的「动态」页签">${list.length}</span></div>
      <ul class="team-events">${rows}</ul>
    </section>`;
}

// formatEventTime 只取时间部分（HH:MM）：跨时区的完整时间戳在窄栏里没有信息量，
// 而且切片是确定性的，用例不会随机器时区变色。
export function formatEventTime(at) {
  const text = String(at || "").trim();
  const match = /^\d{4}-\d{2}-\d{2}T(\d{2}:\d{2})/.exec(text);
  return match ? match[1] : (text ? truncate(text, 11) : "—");
}

function truncate(text, limit) {
  const value = String(text ?? "");
  return value.length > limit ? `${value.slice(0, limit - 1)}…` : value;
}

// TEAM_BOARD_CSS 是这块看板**与它的详情页**自己的样式（只吃 styles.css 的语义 token，不写
// 第二套色值）。接线进右栏时把它注入一个 <style>（见 app.js 的 ensureTeamBoardStyles），
// 静态预览页直接注入这一段——两份消费面，一份定义。
//
// 上一版（真甘特 · 边与箭头）的几何整条退场：`.team-dag-edge*` / `.team-dag-grid` /
// `.team-dag-row` / `.team-dag-frame*` / `.team-dag-gate*` 全部删除。留下来就是"没人调的
// 第二套几何"，而它按行高实测（layoutTeamGanttEdges）——留着会让人以为 y 还需要量。
// 现在**没有 y 轴**：行高由内容撑，格子的位置只有 x（槽位算式与一条 CSS 底纹）。
export const TEAM_BOARD_CSS = `
/* 作用域：看板的令牌定义在 .team-board 上，**详情页**的令牌定义在 .team-page 上——两处共用
   同一个选择器列表里的同一份色板（详情页活在弹窗里，不在 .team-board 的子树下）。
   - 几何（--team-dag-*）：与渲染件的槽位算式逐字对应（x = 槽 × slot-w）；
   - teammate 色板（--team-dag-role-0..5）：本件自造（既有令牌里没有"归属色"这一族）；
   - 里程碑淡色（--team-dag-ms-tone-0..4）：同上，低饱和、刻意避开四状态色。
   状态色/淡色底一律引用既有 --status-* / --tint-* / --border-*，不另造 hex。
   container-type 让窄栏（产品右栏 ~360px）能用 @container 收缩，不用另加一套裁剪口径。 */
.team-board, .team-page {
  --team-dag-scroll-max-h: 380px;
  --team-dag-slots: 1;
  --team-dag-label-w: 176px;
  --team-dag-slot-w: 56px;
  --team-dag-bar-h: 18px;
  --team-dag-row-h: 46px;
  --team-dag-ruler-h: 30px;
  --team-dag-role-0: #6f5bd6;
  --team-dag-role-1: #a4538f;
  --team-dag-role-2: #0f8f9e;
  --team-dag-role-3: #6b7f2e;
  --team-dag-role-4: #4b4fa8;
  --team-dag-role-5: #5a5f68;
  --team-dag-ms-tone-0: #6a7fa8;
  --team-dag-ms-tone-1: #8a7fa8;
  --team-dag-ms-tone-2: #6f8f7f;
  --team-dag-ms-tone-3: #a08a6a;
  --team-dag-ms-tone-4: #8f6f7f;
}
[data-theme="dark"] .team-board, [data-theme="dark"] .team-page {
  --team-dag-role-0: #a08bf0;
  --team-dag-role-1: #c98fc3;
  --team-dag-role-2: #45c2bd;
  --team-dag-role-3: #a9c46a;
  --team-dag-role-4: #8f93e0;
  --team-dag-role-5: #9aa0aa;
  --team-dag-ms-tone-0: #8fa6c8;
  --team-dag-ms-tone-1: #a89ac4;
  --team-dag-ms-tone-2: #8fb09a;
  --team-dag-ms-tone-3: #c4ab84;
  --team-dag-ms-tone-4: #b894a4;
}
.team-board { display: flex; flex-direction: column; gap: 8px; min-width: 0; font-size: var(--text-sm); color: var(--text-strong); container-type: inline-size; }
.team-board-head { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; font-size: var(--text-xs); color: var(--text-dim); }
.team-badge { flex: none; padding: 1px 5px; border: 1px solid var(--border-strong); border-radius: 4px; font-weight: 600; letter-spacing: .06em; color: var(--text-mid); }
.team-board-id { min-width: 0; max-width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: var(--font-mono); color: var(--text-strong); }
.team-version { flex: none; color: var(--text-dim); }
.team-counts { flex: 1 1 100%; min-width: 0; overflow-wrap: anywhere; }
.team-recovered { border-color: var(--border-strong); color: var(--text-mid); }
.team-stale { border-color: var(--border-running); color: var(--status-running); }
.team-status { flex: none; padding: 0 5px; border: 1px solid var(--border); border-radius: 999px; font-size: 10px; letter-spacing: .04em; text-transform: uppercase; }
.team-status.is-running { color: var(--status-running); border-color: var(--border-running); background: var(--tint-running); }
.team-status.is-active { color: var(--status-running); border-color: var(--border-running); background: var(--tint-running); }
.team-status.is-done { color: var(--status-done); border-color: var(--border-done); background: var(--tint-done); }
.team-status.is-failed { color: var(--status-failed); border-color: var(--border-failed); background: var(--tint-failed); }
.team-status.is-review { color: var(--status-info); border-color: var(--border-info); background: var(--tint-info); }
.team-status.is-pending { color: var(--status-idle); }
.team-status.is-free { color: var(--status-idle); }
.team-label { color: var(--faint); font-size: var(--text-xs); }
.team-role { color: var(--text-mid); border: 1px solid var(--border-hairline); }
.team-dep { color: var(--text-dim); border: 1px dashed var(--border-strong); }
.team-count { padding: 0 5px; border: 1px solid var(--border-hairline); border-radius: 4px; font-size: 10px; }
.team-caption { color: var(--faint); font-size: var(--text-xs); }
.team-dag-id { font-family: var(--font-mono); font-size: 10.5px; font-weight: 700; color: var(--text-strong); }
.team-dag-dot { display: inline-block; width: 7px; height: 7px; border-radius: 50%; background: var(--team-dag-role-color, transparent); }
/* ── 里程碑格栅（wi-milestone-strip，2026-10-08）────────────────────────────────────
   一行一个里程碑：左列读"这块是什么"（名字 / 状态 / 进度 / WI 编号 / 内容摘要 / 屏障），
   右列读"这块占多宽"（格子 + 汇总条）。横轴是依赖槽位，**不是时间**；刻度尺把这句话写在
   脸上。没有任何坐标需要实测：左端 = 槽位 × --team-dag-slot-w，格子底纹由一条 repeating
   gradient 画出来（每 --team-dag-slot-w 一条竖线），宽度永远跟着容器查询一起缩。 */
.team-dag-scroll { max-height: var(--team-dag-scroll-max-h, 380px); overflow: auto; overscroll-behavior: contain; scrollbar-width: thin; scrollbar-color: var(--border-strong) transparent; }
.team-dag-scroll::-webkit-scrollbar { width: 10px; height: 10px; }
.team-dag-scroll::-webkit-scrollbar-track { background: transparent; }
.team-dag-scroll::-webkit-scrollbar-thumb { background: var(--border-strong); border-radius: 999px; border: 2px solid transparent; background-clip: padding-box; }
.team-dag-scroll::-webkit-scrollbar-thumb:hover { background: var(--faint); background-clip: padding-box; }
.team-dag-content { position: relative; display: flex; flex-direction: column; padding-top: 2px; min-width: calc(var(--team-dag-label-w) + var(--team-dag-slots, 1) * var(--team-dag-slot-w)); }
.team-dag-ruler { position: sticky; top: 0; z-index: 6; display: grid; grid-template-columns: var(--team-dag-label-w) 1fr; box-sizing: border-box; height: var(--team-dag-ruler-h); border-bottom: 1px solid var(--border-strong); background: var(--panel-solid); }
.team-dag-ruler-label { display: flex; align-items: center; padding: 0 8px; border-right: 1px solid var(--border); font-family: var(--font-mono); font-size: 10.5px; font-weight: 700; color: var(--text-mid); white-space: nowrap; overflow: hidden; }
.team-dag-ruler-plot { position: relative; overflow: hidden; }
.team-dag-tick { position: absolute; top: 0; bottom: 0; left: calc(var(--i) * var(--team-dag-slot-w)); }
.team-dag-tick i { position: absolute; left: 0; top: 16px; bottom: 0; width: 1px; background: var(--border-strong); }
.team-dag-tick b { position: absolute; left: 3px; top: 15px; font-family: var(--font-mono); font-size: 9.5px; font-weight: 700; color: var(--faint); }
.team-dag-ruler-basis { position: absolute; left: 4px; top: 1px; font-family: var(--font-mono); font-size: 9.5px; font-weight: 700; color: var(--text-dim); white-space: nowrap; }
.team-dag-ms-rows { display: flex; flex-direction: column; }
.team-dag-ms { display: grid; grid-template-columns: var(--team-dag-label-w) 1fr; align-items: stretch; border-top: 1px solid var(--border-hairline); }
.team-dag-ms:first-child { border-top: 0; }
/* 屏障没放行同理三样一起写：左侧色栏转虚线 + 整行降透明度（🔒 由渲染件挂）。 */
.team-dag-ms[data-locked="true"] { opacity: .62; }
.team-dag-ms-label { display: flex; flex-direction: column; justify-content: center; gap: 3px; min-width: 0; padding: 6px 8px 6px 7px; border-left: 3px solid var(--team-dag-ms-tone, var(--border-strong)); border-right: 1px solid var(--border); }
.team-dag-ms[data-locked="true"] .team-dag-ms-label { border-left-style: dashed; }
.team-dag-ms-line { display: flex; align-items: center; gap: 5px; min-width: 0; }
.team-dag-ms-open { flex: 1 1 auto; min-width: 0; padding: 0; border: 0; background: none; text-align: left; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: var(--text-sm); font-weight: 700; color: var(--text-strong); cursor: pointer; }
.team-dag-ms-count { flex: none; font-family: var(--font-mono); font-size: 10px; color: var(--text-mid); }
.team-dag-ms-wis { display: flex; flex-wrap: wrap; gap: 3px; min-width: 0; }
.team-wi { display: inline-flex; align-items: center; gap: 3px; padding: 0 4px; border: 1px solid var(--row-line, var(--border-hairline)); border-radius: 3px; font-family: var(--font-mono); font-size: 9.5px; color: var(--text-mid); }
.team-dag-ms-meta { display: flex; flex-wrap: wrap; align-items: center; gap: 4px; min-width: 0; font-size: 9.5px; color: var(--faint); }
.team-dag-ms-deps { flex: none; font-family: var(--font-mono); color: var(--text-dim); }
.team-dag-ms-summary { min-width: 0; max-width: 100%; overflow: hidden; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; line-height: 1.35; }
.team-dag-lock { flex: none; padding: 0 5px; border: 1px solid var(--border-strong); border-radius: 999px; font-size: 10px; color: var(--status-idle); }
.team-dag-lock.is-open { color: var(--status-done); border-color: var(--border-done); background: var(--tint-done); }
.team-dag-warn { color: var(--status-failed); }
.team-dag-ms-plot { position: relative; min-height: var(--team-dag-row-h); background-image: repeating-linear-gradient(to right, var(--border-hairline) 0 1px, transparent 1px var(--team-dag-slot-w)); }
/* 汇总条：横跨名下工作项的 min(槽)..max(末尾)，底色是里程碑自己的淡色（逐块轮换）。
   空里程碑画一枚零宽菱形 —— 它落在屏障前驱的右端那一格上。 */
.team-dag-ms-span { position: absolute; top: 50%; left: calc(var(--s, 0) * var(--team-dag-slot-w)); width: calc((var(--e, 0) - var(--s, 0)) * var(--team-dag-slot-w) - 2px); height: calc(var(--team-dag-bar-h) + 8px); transform: translateY(-50%); box-sizing: border-box; border: 1px solid color-mix(in srgb, var(--team-dag-ms-tone, transparent) 45%, transparent); border-radius: 5px; background: color-mix(in srgb, var(--team-dag-ms-tone, transparent) 14%, transparent); }
.team-dag-ms-span[data-empty="true"] { width: 9px; height: 9px; border-radius: 2px; transform: translateY(-50%) rotate(45deg); background: var(--team-dag-ms-tone, var(--faint)); }
/* 一格一件事。两条通道正交：**描边 = 状态**（pending 虚线），**填充 = 归属（teammate）**；
   色板回绕（第 7 位起）时填充换斜纹，免得两位撞色只靠色块分不出来（口径 11）。
   **同槽并行**（一格挤 n 件，b→a / c→d 那种形状）→ 这一格横着切成 n 小格：--n 是这一格
   几件、--k 是第几件。不切的话后画的那件会把先画的整件盖住（像素里读不到它的描边）。 */
.team-dag-cell { position: absolute; top: 50%; left: calc(var(--i, 0) * var(--team-dag-slot-w) + (var(--team-dag-slot-w) - 2px) * var(--k, 0) / var(--n, 1) + 1px); box-sizing: border-box; width: calc((var(--team-dag-slot-w) - 2px) / var(--n, 1)); height: var(--team-dag-bar-h); transform: translateY(-50%); border: 1px solid var(--row-line, var(--status-idle)); border-radius: 3px; background: color-mix(in srgb, var(--team-dag-role-color, var(--faint)) 16%, transparent); }
.team-dag-cell.is-wrapped { background: repeating-linear-gradient(45deg, color-mix(in srgb, var(--team-dag-role-color, var(--faint)) 40%, transparent) 0 3px, transparent 3px 7px); }
/* 状态色只有一处判据（渲染件的 effStatus / milestoneEff），这里只做映射，不重算。
   作用域收在 .team-board 里：data-eff 是看板自己的属性，别把它变成全局选择器。 */
.team-board [data-eff="running"] { --row-line: var(--status-running); }
.team-board [data-eff="done"] { --row-line: var(--status-done); }
.team-board [data-eff="review"] { --row-line: var(--status-info); }
.team-board [data-eff="failed"] { --row-line: var(--status-failed); }
.team-board [data-eff="pending"] { --row-line: var(--status-idle); }
.team-dag-cell[data-eff="pending"], .team-wi[data-eff="pending"] { border-style: dashed; }
/* ── teammate 条目（一行一位）────────────────────────────────────────────────── */
.team-section { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
.team-section-title { display: flex; align-items: center; gap: 5px; font-size: var(--text-xs); color: var(--text-dim); }
.team-section-hint { color: var(--faint); font-size: 10px; }
.team-members { display: flex; flex-direction: column; gap: 2px; margin: 0; padding: 0; list-style: none; min-width: 0; }
.team-member { display: flex; align-items: center; gap: 6px; min-width: 0; padding: 3px 6px 3px 5px; border: 1px solid var(--border-hairline); border-left: 3px solid var(--team-dag-role-color, var(--border)); border-radius: 5px; }
.team-member-name { display: inline-flex; align-items: center; gap: 5px; min-width: 0; padding: 0; border: 0; background: none; text-align: left; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: var(--font-mono); font-size: var(--text-sm); color: var(--text-strong); cursor: pointer; }
.team-member-count { flex: none; font-family: var(--font-mono); font-size: 10px; color: var(--text-dim); }
.team-member-wt { flex: 1 1 auto; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; text-align: right; font-family: var(--font-mono); font-size: 10px; color: var(--faint); }
.team-member-session { flex: none; padding: 0 5px; border: 1px solid var(--border-info); border-radius: 4px; font-size: 10px; color: var(--status-info); }
.team-policy { flex: none; padding: 0 5px; border: 1px solid var(--border-hairline); border-radius: 4px; font-size: 10px; color: var(--text-mid); }
.team-policy.is-readonly { color: var(--status-info); border-color: var(--border-info); background: var(--tint-info); }
.team-queue-item { padding: 0 5px; border: 1px solid var(--border-hairline); border-radius: 4px; font-size: var(--text-xs); color: var(--text-mid); }
.team-member-chip { display: inline-flex; align-items: center; gap: 4px; }
/* ── 审计尾巴（侧边栏）─────────────────────────────────────────────────────── */
.team-events, .team-item-messages { display: flex; flex-direction: column; gap: 3px; margin: 0; padding: 0; list-style: none; min-width: 0; }
.team-event { display: flex; align-items: baseline; gap: 5px; flex-wrap: wrap; min-width: 0; font-size: var(--text-xs); }
.team-event-kind { padding: 0 5px; border: 1px solid var(--border-hairline); border-radius: 4px; font-size: 10px; color: var(--text-mid); }
.team-event-kind.is-dispatch { color: var(--status-info); border-color: var(--border-info); }
.team-event-kind.is-milestone { color: var(--status-done); border-color: var(--border-done); }
.team-event-kind.is-retire { color: var(--text-dim); }
.team-event-target { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: var(--font-mono); color: var(--text-mid); }
.team-event-at { flex: none; color: var(--faint); }
.team-event-detail { flex: 1 1 100%; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--faint); }
.team-item-message { display: flex; gap: 5px; min-width: 0; font-size: var(--text-xs); color: var(--text-mid); }
/* ── 详情页（里程碑 / Work Item / teammate 共用一套壳）────────────────────────── */
.team-page-card { width: min(760px, 100%); max-height: min(82vh, 760px); overflow: auto; }
.team-page { display: flex; flex-direction: column; gap: 10px; min-width: 0; font-size: var(--text-sm); color: var(--text-strong); }
.team-page.is-empty { color: var(--text-dim); }
.team-page-head { display: flex; flex-wrap: wrap; align-items: center; gap: 7px; min-width: 0; }
.team-page-kind { flex: none; padding: 1px 6px; border: 1px solid var(--border-strong); border-radius: 4px; font-size: 10px; letter-spacing: .06em; text-transform: uppercase; color: var(--text-mid); }
.team-page-name { margin: 0; min-width: 0; font-size: var(--text-md); font-weight: 600; color: var(--text-bright); }
.team-page-bar { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; padding-bottom: 6px; border-bottom: 1px solid var(--border); }
.team-page-root { flex: none; font-size: var(--text-xs); color: var(--faint); }
.team-page-back { flex: none; padding: 1px 7px; border: 1px solid var(--border-hairline); border-radius: 999px; background: none; color: var(--text-mid); font-size: var(--text-xs); cursor: pointer; }
.team-page-back:hover { color: var(--text-bright); border-color: var(--border-strong); }
.team-page-tabs { display: flex; align-items: center; gap: 4px; flex-wrap: wrap; min-width: 0; }
.team-page-tab { padding: 2px 9px; border: 1px solid transparent; border-radius: 999px; background: none; color: var(--text-mid); font-size: var(--text-xs); cursor: pointer; }
.team-page-tab:hover { color: var(--text-bright); border-color: var(--border-hairline); }
.team-page-tab.is-active { color: var(--text-strong); border-color: var(--border-strong); background: var(--panel); }
.team-page-panels { min-width: 0; }
/* 面板**全部渲染出来**，切页签只是 DOM 上加一个类（见渲染件的 teamPageShell）：不切数据、
   不重算，所以"切回去"永远不会给出与第一次不同的读数。 */
.team-page-panel { display: none; }
.team-page-panel.is-active { display: block; }
.team-page-sub { margin: 12px 0 4px; font-size: var(--text-xs); color: var(--text-dim); }
.team-page-note { margin: 4px 0; color: var(--text-dim); font-size: var(--text-xs); line-height: 1.6; }
.team-page-warn { margin: 4px 0; color: var(--status-failed); font-size: var(--text-xs); }
.team-kv { display: grid; gap: 2px; }
.team-kv-row { display: grid; grid-template-columns: minmax(84px, 128px) 1fr; gap: 8px; align-items: baseline; min-width: 0; }
.team-kv-key { color: var(--faint); font-size: var(--text-xs); }
.team-kv-value { min-width: 0; overflow-wrap: anywhere; color: var(--text-mid); }
.team-chip-row { display: flex; flex-wrap: wrap; align-items: center; gap: 4px; margin: 8px 0 0; min-width: 0; }
.team-link { padding: 0; border: 0; background: none; color: var(--text-strong); font: inherit; font-family: var(--font-mono); font-size: 0.95em; cursor: pointer; text-decoration: underline dotted var(--border-strong); text-underline-offset: 2px; }
.team-link:hover { color: var(--text-bright); }
.team-table { width: 100%; border-collapse: collapse; font-size: var(--text-xs); }
.team-table th { padding: 4px 6px; border-bottom: 1px solid var(--border); text-align: left; font-weight: 600; color: var(--faint); }
.team-table td { padding: 4px 6px; border-bottom: 1px solid var(--border-hairline); color: var(--text-mid); vertical-align: top; }
.team-dep-list { display: grid; gap: 2px; margin: 0; padding: 0; list-style: none; font-size: var(--text-xs); }
/* ── 插件装配（2026-10-05）：声明面 chips + 生效读数 + 黄牌 / 失灵（teammate 级） ── */
.team-member-assembly { display: flex; align-items: center; gap: 4px; flex-wrap: wrap; min-width: 0; font-size: var(--text-xs); }
.team-assembly-plugin { max-width: 160px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; padding: 0 5px; border: 1px solid var(--border-hairline); border-radius: 4px; font-size: var(--text-xs); color: var(--text-mid); }
.team-assembly-readout { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: var(--font-mono); color: var(--text-dim); }
.team-assembly-inherit { padding: 0 5px; border: 1px dashed var(--border-strong); border-radius: 4px; color: var(--text-dim); }
.team-assembly-scope { color: var(--faint); font-size: var(--text-xs); }
.team-assembly-note { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-mid); }
.team-assembly-yellow { color: var(--status-running); border-color: var(--border-running); background: var(--tint-running); }
.team-assembly-faulted { color: var(--status-failed); border-color: var(--border-failed); background: var(--tint-failed); }
.team-assembly-missing { color: var(--status-info); border-color: var(--border-info); }
.team-interrupted { color: var(--status-failed); border-color: var(--border-failed); }
.team-blocked { color: var(--status-idle); }
.team-live { color: var(--status-done); border-color: var(--border-done); }
.team-live-pager { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; margin-top: 6px; font-size: var(--text-xs); color: var(--text-dim); }
.team-live-page { padding: 0 6px; border: 1px solid var(--border-hairline); border-radius: 4px; background: none; color: var(--text-mid); cursor: pointer; }
.team-live-page[disabled] { opacity: .4; cursor: default; }
.team-item-note { color: var(--text-dim); font-size: var(--text-xs); line-height: 1.6; }
.team-item-panel .role-kv-row { display: flex; gap: 6px; min-width: 0; }
.team-item-panel .role-kv-key { flex: none; min-width: 72px; color: var(--faint); }
.team-item-panel .role-kv-value { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-mid); }
/* 窄栏自适应（口径 10）：产品右栏只有 ~360px。几何整体压小（格子的 x 全是槽位算式，跟着一起
   缩）；折叠的只是次级信息（屏障 deps 与内容摘要的第二行），全文仍在 title 与详情页里。 */
@container (max-width: 520px) {
  .team-dag-scroll { --team-dag-slot-w: 38px; --team-dag-label-w: 124px; --team-dag-bar-h: 15px; --team-dag-row-h: 42px; --team-dag-ruler-h: 28px; }
  .team-dag-ms-deps, .team-dag-ms-summary, .team-section-hint { display: none; }
}
`;