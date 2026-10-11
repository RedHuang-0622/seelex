import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const embedURL = `data:text/javascript;base64,${Buffer.from(await readFile(new URL("./html-embed.js", import.meta.url), "utf8")).toString("base64")}`;
// markdown.js 现在也 import ./file-reference.js（引用 chip 的渲染）：data: URL 模块
// 解析不了相对路径，两个 specifier 都要换成可解析的 URL。
const fileReferenceURL = new URL("./file-reference.js", import.meta.url).href;
const markdownSource = (await readFile(new URL("./markdown.js", import.meta.url), "utf8"))
  .replace('"./html-embed.js"', `"${embedURL}"`)
  .replace('"./file-reference.js"', `"${fileReferenceURL}"`);
const markdownURL = `data:text/javascript;base64,${Buffer.from(markdownSource).toString("base64")}`;
const componentSource = (await readFile(new URL("./components.js", import.meta.url), "utf8"))
  .replace('"./markdown.js"', `"${markdownURL}"`);
const { renderChatActivity, renderConversationComponent, renderConversationModel, renderMessageQueue, messageRoleClass, roleIdentity, DRAFT_ROW_KEY, COMPACTION_FRONTIER_KEY } = await import(`data:text/javascript;base64,${Buffer.from(componentSource).toString("base64")}`);

test("assigns each message the identity of the agent that owns the round", () => {
  assert.equal(roleIdentity({ role: "assistant", role_name: "main" }), "EXEC");
  assert.equal(roleIdentity({ role: "assistant", role_name: "tl" }), "ADVISOR");
  assert.equal(roleIdentity({ role: "assistant", role_name: "techlead" }), "ADVISOR");
  assert.equal(roleIdentity({ role: "assistant", role_name: "reviewer" }), "REVIEWER");
  assert.equal(roleIdentity({ role: "assistant" }), "AGENT");
  assert.equal(roleIdentity({ role: "user", role_name: "user" }), "YOU");

  const rendered = renderConversationComponent(
    [{ id: "m-1", role: "assistant", role_name: "tl", round_id: 3, content: "verdict: done" }],
    { running: false }
  );
  assert.match(rendered.html, /<strong>ADVISOR<\/strong>/);
  assert.match(rendered.html, />R3</);
  assert.match(rendered.html, /data-role-name="tl"/);
  assert.doesNotMatch(rendered.html, /<strong>AGENT<\/strong>/);
});

test("distinguishes EXEC and ADVISOR rounds by their own background band", () => {
  const rendered = renderConversationComponent([
    { id: "m-1", role: "assistant", role_name: "tl", content: "裁决：not_done" },
    { id: "m-2", role: "assistant", role_name: "main", content: "本轮由 EXEC 主持" },
    { id: "m-3", role: "assistant", content: "普通回复" }
  ], { running: false });
  // 两个 agent 的 provider role 都是 assistant：靠归属 class 分开上色。
  assert.match(rendered.html, /class="message assistant is-advisor"/);
  assert.match(rendered.html, /class="message assistant is-exec"/);
  // 无归属的普通 assistant 不套背景带（既有观感不变）。
  assert.match(rendered.html, /class="message assistant" data-conversation-key/);
  assert.equal(messageRoleClass({ role: "assistant" }), "");
  assert.equal(messageRoleClass({ role_name: "tl" }), " is-advisor");
  assert.equal(messageRoleClass({ role_name: "main" }), " is-exec");
  assert.equal(messageRoleClass({ role_name: "reviewer" }), " is-role");
});

test("renders runtime activity only from active chat state", () => {
  assert.equal(renderChatActivity({ running: false }), "");
  const html = renderChatActivity({ running: true });
  assert.match(html, /class="runtime-activity"/);
  assert.match(html, /执行中/);
});

test("renders queued inputs as one-line rows", () => {
  const html = renderMessageQueue({
    running: true,
    input_queue: ["**follow up**", "<script>alert(1)</script>"]
  });

  // 一行一条（Qoder 形状）：拖拽把手 + 折返箭头 + 单行正文 + 右侧动作；序号进可访问名，不占纸面。
  assert.equal((html.match(/class="queued-message-text"/g) || []).length, 2);
  assert.equal((html.match(/class="queued-message-lead"/g) || []).length, 2);
  assert.equal((html.match(/class="queued-drag-handle"/g) || []).length, 2);
  assert.match(html, /aria-label="排队 01，等待发送"/);
  assert.match(html, /aria-label="排队 02，等待发送"/);
  // 正文是纯文本：单行省略号要求这段行内内容属于该元素本身，markdown 的块级 <p> 会让
  // text-overflow 失效——所以 `**` 原样显示，不再是 <strong>。
  assert.match(html, /\*\*follow up\*\*/);
  assert.doesNotMatch(html, /<strong>follow up<\/strong>/);
  // 逃逸仍成立（正文里的标签只以文本出现），且完整原文留给悬停提示（data-tip）。
  assert.doesNotMatch(html, /<script>/);
  assert.match(html, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/);
  assert.match(html, /data-tip="&lt;script&gt;alert\(1\)&lt;\/script&gt;"/);
});

test("appends activity after conversation without changing tool payloads", () => {
  const rendered = renderConversationComponent(
    [{ role: "assistant", content: "answer" }],
    { running: true, input_queue: ["next"] }
  );

  // 对话流里只剩"执行中"活动带：排队条自成一叠、宿主是输入框上沿的
  // #message-queue（app.js/chat-view.js），不再随对话滚动跑掉。
  assert.match(rendered.html, /class="message assistant"[\s\S]*runtime-activity/);
  assert.doesNotMatch(rendered.html, /排队 01/);
  assert.equal(rendered.payloads.size, 0);
  assert.match(renderMessageQueue({ running: true, input_queue: ["next"] }), /排队 01/);
});

test("uses stable message and tool keys for incremental rendering", () => {
  const model = renderConversationModel([
    { id: "assistant-1", role: "assistant", content: "answer" },
    { id: "tool-start", role: "tool", tool: { id: "call-1", name: "read", arguments: "{}" } },
    { id: "tool-end", role: "tool_result", tool: { id: "call-1", name: "read", result: "done" } }
  ], { running: true });

  assert.deepEqual(model.items.map(item => item.key), ["message:assistant-1", "roll:tool:call-1", "chat:activity"]);
  // 轴卡是"工具过程块"，F-6 会给它挂邻接中介类（这里紧跟 llm 块 ⇒ is-joined-above），
  // 所以只钉 is-tools 本身，不钉 class 属性的结尾。
  assert.match(model.items[1].html, /class="conversation-axis is-tools/);
  assert.match(model.items[1].html, /data-conversation-key="tool:call-1"/);
  assert.match(model.items[1].html, /data-trajectory-key="tool:call-1"/);
  assert.match(model.items[1].html, /工具过程/);
});

test("renders tool calls as one-line chips without IN/OUT panels", () => {
  const model = renderConversationModel([
    { id: "tool-start", role: "tool", tool: { id: "call-big", name: "bash", arguments: "{}" } },
    { id: "tool-end", role: "tool_result", tool: {
      id: "call-big", name: "bash", result: "preview",
      result_ref: "tr-abc123", truncated: true, total_chars: 9000
    } }
  ]);

  const item = model.items.find(entry => entry.html.includes('data-conversation-key="tool:call-big"'));
  assert.ok(item, "tool item must be present");
  assert.match(item.html, /class="chat-chip is-tool"/);
  assert.match(item.html, /data-trajectory-key="tool:call-big"/);
  assert.match(item.html, /8\.8 KB/);
  assert.doesNotMatch(item.html, /data-load-ref/);
  assert.doesNotMatch(item.html, /io-collapse/);
  assert.doesNotMatch(item.html, /io-panel/);
  assert.match(item.html, /class="item-id">call-big</);
});

test("keeps chat tool rows to a single line with status", () => {
  const model = renderConversationModel([
    { id: "tool-start", role: "tool", tool: { id: "call-small", name: "read_file", arguments: "{}" } },
    { id: "tool-end", role: "tool_result", tool: { id: "call-small", name: "read_file", result: "small" } }
  ]);
  const item = model.items.find(entry => entry.html.includes('data-conversation-key="tool:call-small"'));
  assert.ok(item);
  assert.match(item.html, /class="tool-state">.*OK/);
  assert.doesNotMatch(item.html, /data-copy/);
  assert.doesNotMatch(item.html, /io-collapse/);
});

test("keeps restored tool steps in message order instead of piling tools then replies", () => {
  // 恢复路径（sessionstore 派生 conversation）产出的事实形状：助手步骤只有
  // 思考，工具调用与结果各自成条，回合末尾才是正文。
  const model = renderConversationModel([
    { id: "message-1", role: "user", content: "长会话问题" },
    { id: "seq-2", role: "assistant", reasoning_content: "先读文件" },
    { id: "seq-2#tool-1", role: "tool", tool: { id: "call-1", name: "read", arguments: "{}", status: "success" } },
    { id: "message-3", role: "tool_result", content: "package a", tool: { id: "call-1", name: "read", result: "package a", status: "success" } },
    { id: "message-4", role: "assistant", content: "结论" }
  ]);

  // 顺序 = 问题 → 助手步骤（含思考）→ 这一步的工具过程 → 正文。
  assert.deepEqual(model.items.map(item => item.meta.kind), ["message", "message", "axis", "message"]);
  assert.match(model.items[1].html, /先读文件/);
  assert.match(model.items[2].html, /工具过程/);
  assert.match(model.items[2].html, /call-1/);
  assert.match(model.items[3].html, /结论/);
  // 工具过程夹在步骤与正文之间，而不是被堆到整段对话末尾。
  const html = model.items.map(item => item.html).join("");
  assert.ok(html.indexOf("先读文件") < html.indexOf("工具过程"));
  assert.ok(html.indexOf("工具过程") < html.indexOf("结论"));
});

test("renders thinking in its own scroll axis and keeps the reply content inline", () => {
  const model = renderConversationModel([
    { id: "a1", role: "assistant", content: "reply", reasoning_content: "thinking steps" }
  ]);
  const item = model.items[0];
  assert.ok(item);
  assert.match(item.html, /class="reasoning-block is-thinking-axis"/);
  assert.match(item.html, /data-trajectory-key="message:a1"/);
  assert.match(item.html, /thinking steps/);
  assert.match(item.html, /class="item-id">a1</);
  assert.match(item.html, />reply<\/p>/);
  assert.match(item.html, /思考/);
});

// 草稿行的 helper：只关心模型层（页面 context 的第二半），渲染细节交给上面的
// renderConversationComponent 用例。
function renderedModel(messages, chat, draft) {
  return renderConversationModel(messages, chat, draft);
}

test("页面 context = 既定 message + 一条可区分的未发送草稿行", () => {
  const rendered = renderConversationComponent(
    [{ id: "m-1", role: "user", role_name: "user", content: "既定消息" }],
    { running: false },
    "还没发出去的字"
  );
  // 两半在同一页上，且草稿排在既定消息之后。
  assert.match(rendered.html, /既定消息/);
  assert.match(rendered.html, /还没发出去的字/);
  assert.ok(
    rendered.html.indexOf("既定消息") < rendered.html.indexOf("还没发出去的字"),
    "草稿是这一页的最后一条（还没提交出去的那一份）"
  );
  // 身份可判：kind=draft + is-draft 类 + data-draft/data-unsent 标记，不靠正文猜。
  const draftItem = renderedModel([], {}, "还没发出去的字").items.at(-1);
  assert.equal(draftItem.key, DRAFT_ROW_KEY);
  assert.equal(draftItem.meta.kind, "draft");
  assert.match(draftItem.html, /class="queued-message draft-message is-draft"/);
  assert.match(draftItem.html, /data-draft="true"/);
  assert.match(draftItem.html, /data-unsent="true"/);
  assert.match(draftItem.html, /未发送草稿/);
  // 草稿是投影：不派 message id（否则会被当成会话里的一条消息）。
  assert.doesNotMatch(draftItem.html, /data-trajectory-key="message:/);
});

test("空草稿不留行：页面 context 只剩既定消息，既有 key 不受影响", () => {
  for (const empty of ["", "   ", "\n", "\t ", undefined, null]) {
    const model = renderConversationModel([{ id: "m-1", role: "user", content: "既有" }], {}, empty);
    assert.equal(
      model.items.some(item => item.meta?.kind === "draft"),
      false,
      `草稿=${JSON.stringify(empty)} 不该出行`
    );
    assert.equal(model.items.length, 1);
    assert.match(model.items[0].html, /data-conversation-key="message:m-1"/);
  }
});

test("草稿行是单行纯文本并 escape 危险文本", () => {
  const item = renderedModel([], {}, "<img src=x onerror=alert(1)> **粗体**").items.at(-1);
  // 与排队条同形（单行条）：正文是纯文本，标签只以文本出现；原文与提示都不含标签。
  assert.doesNotMatch(item.html, /<img/);
  assert.match(item.html, /&lt;img/);
  assert.match(item.html, /data-tip="&lt;img src=x onerror=alert\(1\)&gt; \*\*粗体\*\*"/);
  assert.match(item.html, /class="queued-message-text"/);
  assert.doesNotMatch(item.html, /<strong>粗体<\/strong>/);
});

// ── 压缩分界（会话单例）在对话区的那一行 ─────────────────────────
// 落点与文案由 compaction-format.conversationCompactionAnchor 判定；这里只钉渲染
// 与落点：一行、插在被折出的最后一条消息之后、锚点不在本页就不插。

test("分界行插在被折出保留窗口的最后一条消息之后", () => {
  const messages = [
    { id: "message-1", role: "user", content: "一" },
    { id: "message-2", role: "assistant", content: "二" },
    { id: "message-3", role: "user", content: "三" }
  ];
  const anchor = { messageID: "message-2", label: "以上 消息 message-1..message-2已被压缩", title: "t", note: "", frameRef: "tr-1" };
  const model = renderConversationModel(messages, {}, "", anchor);
  const index = model.items.findIndex(item => item.key === COMPACTION_FRONTIER_KEY);
  assert.equal(index, 2);
  assert.equal(model.items[index - 1].key, "message:message-2");
  assert.equal(model.items[index + 1].key, "message:message-3");
  assert.equal(model.items[index].meta.kind, "frontier");
  assert.match(model.items[index].html, /class="conversation-compaction-frontier"/);
  assert.match(model.items[index].html, /以上 消息 message-1\.\.message-2已被压缩/);
  assert.match(model.items[index].html, /data-compact-frame-ref="tr-1"/);
  assert.match(model.items[index].html, /查看压缩帧正文/);
  // 分界不是消息：不带轨迹 key，免得被当成会话里的一条。
  assert.doesNotMatch(model.items[index].html, /data-trajectory-key/);
});

test("锚点不在本页或没有锚点时绝不插行", () => {
  const messages = [{ id: "message-9", role: "user", content: "九" }];
  const none = renderConversationModel(messages, {}, "");
  assert.doesNotMatch(none.items.map(item => item.html).join(""), /conversation-compaction-frontier/);
  // 记录刷新/换会话导致锚点消息不在本页：宁可少一行，也不插在错的位置。
  const stale = renderConversationModel(messages, {}, "", { messageID: "message-1", label: "x", title: "t", note: "", frameRef: "" });
  assert.doesNotMatch(stale.items.map(item => item.html).join(""), /conversation-compaction-frontier/);
});

test("分界行 escape 说明文字与 ref", () => {
  const anchor = {
    messageID: "message-1",
    label: "以上 <script>alert(1)</script>已被压缩",
    title: "<b>tip</b>",
    note: "<i>2 次</i>",
    frameRef: "tr-\"1\""
  };
  const html = renderConversationModel([{ id: "message-1", role: "user", content: "一" }], {}, "", anchor)
    .items.map(item => item.html).join("");
  assert.doesNotMatch(html, /<script>/);
  assert.match(html, /&lt;script&gt;/);
  assert.match(html, /data-compact-frame-ref="tr-&quot;1&quot;"/);
});

test("message 块第二行元数据：id + config role + config model（真相源字段）", () => {
  const rendered = renderConversationComponent([
    {
      id: "message-836", role: "assistant", content: "回答",
      config_role: "agent", config_model: "glm-5.3-flash", config_provider: "openai"
    }
  ], { running: false });
  assert.match(rendered.html, /class="message-route-meta"/);
  assert.match(rendered.html, /class="item-id">message-836</);
  assert.match(rendered.html, /route-meta-role"[^>]*>agent</);
  assert.match(rendered.html, /route-meta-model"[^>]*>openai\/glm-5\.3-flash</);
  // 顶行仍是角色身份（EXEC/AGENT/…），元数据行不抢角色行。
  assert.match(rendered.html, /class="message-head"/);
});

test("message blocks without route metadata render the id-only meta row", () => {
  const rendered = renderConversationComponent([
    { id: "a-1", role: "assistant", content: "历史回放的旧块" }
  ], { running: false });
  // 旧块没有路由段：元数据行只有 message id（与旧 message-debug 同信息量）。
  assert.match(rendered.html, /class="message-route-meta"/);
  assert.match(rendered.html, /class="item-id">a-1</);
  assert.doesNotMatch(rendered.html, /route-meta-role/);
  assert.doesNotMatch(rendered.html, /route-meta-model/);
  // 没有 id 的消息（罕见形状）不渲染空元数据行。
  const anonymous = renderConversationComponent([
    { role: "assistant", content: "无 id" }
  ], { running: false });
  assert.doesNotMatch(anonymous.html, /message-route-meta/);
});

// ── F-6 邻接圆角：融合 vs 分割 ────────────────────────────────────
// 判据在 components.js 的 blocksFuse：不同类（llm 正文块 ↔ 工具过程块）**始终融合**；
// 同类**默认不分割**，换轮次（round_id）或换角色归属（role_name）＝分割。中介类同
// 时写 data-*（三枚类只出一枚），因此断言既有类也有数据。

test("llm 块紧邻工具过程块：交界两侧圆角置零（融合）", () => {
  const model = renderConversationModel([
    { id: "m-1", role: "assistant", round_id: 4, role_name: "main", content: "先看一眼" },
    { id: "t-1", role: "tool", tool: { id: "call-1", name: "read", arguments: "{}", status: "success" } },
    { id: "t-2", role: "tool_result", content: "done", tool: { id: "call-1", name: "read", result: "done", status: "success" } }
  ]);
  assert.deepEqual(model.items.map(item => item.meta.kind), ["message", "axis"]);
  const [message, axis] = model.items;

  // 上块（llm）只接住下一条：外侧（上）角保留圆角 ⇒ 只有 is-joined-below。
  assert.match(message.html, /class="message assistant is-exec is-joined-below"/);
  assert.match(message.html, /data-joined-below="true"/);
  assert.match(message.html, /data-joined-above="false"/);
  // 下块（工具过程轴卡）只接住上一条。
  assert.match(axis.html, /class="conversation-axis is-tools is-joined-above"/);
  assert.match(axis.html, /data-joined-above="true"/);
  assert.match(axis.html, /data-joined-below="false"/);
  // 融合只加中介类：轴卡的 key 与轮轴数据一字不动。
  assert.equal(axis.key, "roll:tool:call-1");
  assert.match(axis.html, /data-conversation-key="roll:tool:call-1"/);
  assert.match(axis.html, /data-wheel-kind="tools"/);
});

test("工具过程夹在两枚同归属 llm 块之间：中段出 is-joined-both", () => {
  const model = renderConversationModel([
    { id: "m-1", role: "assistant", round_id: 2, role_name: "tl", content: "第一段" },
    { id: "t-1", role: "tool", tool: { id: "call-9", name: "bash", arguments: "{}", status: "success" } },
    { id: "t-2", role: "tool_result", content: "ok", tool: { id: "call-9", name: "bash", result: "ok", status: "success" } },
    { id: "m-2", role: "assistant", round_id: 2, role_name: "tl", content: "第二段" }
  ]);
  assert.deepEqual(model.items.map(item => item.meta.kind), ["message", "axis", "message"]);
  const [first, axis, last] = model.items;

  assert.match(first.html, /data-joined-above="false"/);
  assert.match(first.html, /data-joined-below="true"/);
  assert.doesNotMatch(first.html, /is-joined-above|is-joined-both/);
  // 中段：上下都融合。交界两侧都置 0，中段块自己的外侧被邻居的圆角接手。
  assert.match(axis.html, /class="conversation-axis is-tools is-joined-both"/);
  assert.match(axis.html, /data-joined-above="true"/);
  assert.match(axis.html, /data-joined-below="true"/);
  // 末块只接住上面一条 ⇒ 融合组的下外侧角仍是圆角。
  assert.match(last.html, /class="message assistant is-advisor is-joined-above"/);
  assert.match(last.html, /data-joined-above="true"/);
  assert.match(last.html, /data-joined-below="false"/);
});

test("同类 llm 块：同轮次同归属默认不分割，换轮次 / 换归属则分割", () => {
  // 同一轮、同一归属：默认不分割 ⇒ 两块在交界处都没有圆角。
  const same = renderConversationModel([
    { id: "a-1", role: "assistant", round_id: 7, role_name: "main", content: "上半" },
    { id: "a-2", role: "assistant", round_id: 7, role_name: "main", content: "下半" }
  ]);
  assert.deepEqual(same.items.map(item => item.meta.kind), ["message", "message"]);
  assert.match(same.items[0].html, /class="message assistant is-exec is-joined-below"/);
  assert.match(same.items[1].html, /class="message assistant is-exec is-joined-above"/);

  // 换轮次 = 分割：两边都不加融合类（两枚块圆角相对、中间保留间距）。
  const nextRound = renderConversationModel([
    { id: "b-1", role: "assistant", round_id: 7, role_name: "main", content: "上" },
    { id: "b-2", role: "assistant", round_id: 8, role_name: "main", content: "下" }
  ]);
  for (const item of nextRound.items) {
    assert.doesNotMatch(item.html, /is-joined/);
    assert.match(item.html, /data-joined-above="false"/);
    assert.match(item.html, /data-joined-below="false"/);
  }

  // 同一轮里换角色归属（EXEC 主持 → ADVISOR 裁决）= 分割。
  const roleSwap = renderConversationModel([
    { id: "c-1", role: "assistant", round_id: 9, role_name: "main", content: "主持" },
    { id: "c-2", role: "assistant", round_id: 9, role_name: "tl", content: "裁决" }
  ]);
  for (const item of roleSwap.items) assert.doesNotMatch(item.html, /is-joined/);
  assert.match(roleSwap.items[1].html, /class="message assistant is-advisor"/);
});

test("自成一段的行（运行时活动带）不参与融合", () => {
  const model = renderConversationModel([
    { id: "d-1", role: "assistant", round_id: 3, role_name: "main", content: "答" }
  ], { running: true });
  const tail = model.items.find(item => item.key === "chat:activity");
  assert.ok(tail, "活动带必须还在");
  // 活动带不是块：既没有中介类，也不带邻接数据。
  assert.doesNotMatch(tail.html, /is-joined/);
  assert.doesNotMatch(tail.html, /data-joined/);
  // 它也不把上面的 llm 块拖进"融合"。
  assert.match(model.items[0].html, /data-joined-below="false"/);
  assert.doesNotMatch(model.items[0].html, /is-joined/);
});

// cssRules 剥掉注释后展开「选择器 → 声明体」（口径同 style-baseline.test.mjs）。
function cssRules(css) {
  const bare = css.replace(/\/\*[\s\S]*?\*\//g, "");
  const rules = [];
  for (const match of bare.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    if (match[1].trim().startsWith("@")) continue;
    rules.push({ selector: match[1].trim(), body: match[2] });
  }
  return rules;
}

test("styles.css 消费邻接中介类：交界圆角与间距归零，外侧角不动", async () => {
  const rules = cssRules(await readFile(new URL("./styles.css", import.meta.url), "utf8"));
  const joinedRules = rules.filter(rule => rule.selector.includes("is-joined"));
  assert.ok(joinedRules.length >= 2, "styles.css 必须有邻接中介类的规则");
  const declares = (selector, property) => joinedRules.some(rule =>
    rule.selector.split(",").map(part => part.trim()).includes(selector) &&
    new RegExp(property).test(rule.body));

  // 交界侧：上块的底圆角 / 下块的顶圆角置 0，且间距归零（融合成一块）。
  for (const block of [".message", ".conversation-axis", ".tool-run"]) {
    assert.ok(declares(`${block}.is-joined-below`, "margin-bottom:\\s*0"), `${block} 融合下侧要归零间距`);
    assert.ok(declares(`${block}.is-joined-below`, "border-bottom-left-radius:\\s*0"), `${block} 下圆角要置 0`);
    assert.ok(declares(`${block}.is-joined-below`, "border-bottom-right-radius:\\s*0"), `${block} 下圆角要置 0`);
    assert.ok(declares(`${block}.is-joined-above`, "margin-top:\\s*0"), `${block} 融合上侧要归零间距`);
    assert.ok(declares(`${block}.is-joined-above`, "border-top-left-radius:\\s*0"), `${block} 上圆角要置 0`);
    assert.ok(declares(`${block}.is-joined-above`, "border-top-right-radius:\\s*0"), `${block} 上圆角要置 0`);
  }
  // llm 块的"框"长在 .message-body 上（user 气泡取 --r-xl）：交界那一侧同样置 0。
  assert.ok(declares(".message.is-joined-above .message-body", "border-top-left-radius:\\s*0"));
  assert.ok(declares(".message.is-joined-below .message-body", "border-bottom-right-radius:\\s*0"));

  // 融合规则只许"置 0"：不写死半径（半径只取 --r-*，B0 规则 1），也不把外侧角改成
  // 别的半径——外侧角继续吃基础规则的圆角。
  for (const rule of joinedRules) {
    assert.doesNotMatch(rule.body, /border-radius:\s*[0-9.]+px/, `邻接规则不得写死像素半径：${rule.selector}`);
    assert.doesNotMatch(rule.body, /border-(top|bottom)-(left|right)-radius:\s*var\(/, `外侧角不许被改写：${rule.selector}`);
  }
});
