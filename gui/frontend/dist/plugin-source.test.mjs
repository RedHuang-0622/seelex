import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { escapeHtml } from "./components.js";
import { escapePluginSourceText, pluginSourceBadge, shortenRootPath, shortenSourceUrl, submitPluginNames, withPluginAssembly } from "./plugin-source.js";

// wi-editor 提交侧接线的验收（leader 2026-10-05 裁决）：
//   ① 提交载荷带 plugins；② 空 = 不写该键；③ 重复项**原样提交**，由后端显式拒绝。
//
// 2026-10-12 J-1a（Agent Team 面板退场）：`agent-team-view.js` 已删，所以原
//   ⑩ "提交侧 ↔ 回读侧只差去重"的对照（回读侧的 normalizePluginNames 随面板一起退场）与
//   ⑪ hirePanel 的装配上限提示（表单随面板一起退场）两条用例**删除**——它们断言的对象
//   已经不存在，留着只能改成空壳。
//   "两条提交路都带上装配"那条接线用例同理**改写成退场守卫**：面板没了，app.js 里不该
//   再有任何装配提交路（留一半 = 永远不生效的死接线）。
// 保留下来的都是仍然活着的东西：`plugin-source.js` 的纯函数口径（①②③④⑤⑥⑦⑨）+ 插件页
// 的接线（⑧）。
// 注：`withPluginAssembly` / `submitPluginNames` 目前**没有前端消费方**（唯一的消费方是随
// 面板退场的员工入库 / 入职路）；是否连同 AgentTeam* RPC 面一起收口归后端条目 wi-j1a-be
// 裁决（J-1b 可能另有装配入口），本条目不动 plugin-source.js。
const appSource = await readFile(new URL("./app.js", import.meta.url), "utf8");

test("① 提交载荷带 plugins：表单输入按 逗号/顿号/空格/分号 切分并 trim", () => {
  assert.deepEqual(submitPluginNames(" cad, docs、design "), ["cad", "docs", "design"]);
  assert.deepEqual(submitPluginNames("cad docs；design;draw"), ["cad", "docs", "design", "draw"]);

  const payload = withPluginAssembly({ role_name: "exec" }, " cad , docs ");
  assert.deepEqual(payload.plugins, ["cad", "docs"]);
  assert.equal(payload.role_name, "exec", "不得动别的字段");
  // 键名就是协议名（dto.RoleSpec.Plugins 的 json 标签），不是 camelCase 变体。
  assert.deepEqual(Object.keys(payload).filter(key => key.toLowerCase().includes("plugin")), ["plugins"]);
});

test("① 员工池（数组来源）也走同一份规整：入库/入职搬人时装配跟着走", () => {
  const payload = withPluginAssembly({ role_name: "exec" }, [" cad ", "", "docs"]);
  assert.deepEqual(payload.plugins, ["cad", "docs"]);
  assert.deepEqual(submitPluginNames(["", "   "]), null, "只剩空项 = 没清单");
});

test("② 空 = 不写该键（不覆盖），不是空数组", () => {
  for (const empty of ["", "   ", ",", "、 ； ", [], null, undefined]) {
    const payload = withPluginAssembly({ role_name: "exec" }, empty);
    assert.equal(Object.hasOwn(payload, "plugins"), false, `空值 ${JSON.stringify(empty)} 不得写键`);
    assert.equal("plugins" in payload, false);
  }
  assert.equal(submitPluginNames("   "), null);
  // JSON 序列化后确实没有这个键（后端读到缺失 = 不覆盖）。
  assert.equal(JSON.stringify(withPluginAssembly({ role_name: "exec" }, "  ,  ")), '{"role_name":"exec"}');
});

test("③ 重复项原样提交（前端不静默去重），由后端显式拒绝", () => {
  assert.deepEqual(submitPluginNames("cad, cad，cad"), ["cad", "cad", "cad"]);
  assert.deepEqual(withPluginAssembly({}, "docs; docs").plugins, ["docs", "docs"]);
  assert.deepEqual(submitPluginNames(["cad", " cad ", "docs"]), ["cad", "cad", "docs"], "保序、不吃重复");
  // 后端裁决（一次取证，2026-10-05，探针 tmp/plugin-assembly-probe）：
  //   go run ./tmp/plugin-assembly-probe
  //   B SaveEmployee(重复 {'cad',' cad '}) err=agentteam: 角色 "probe-emp" 的插件装配非法: 插件 "cad" 重复声明（显式拒绝，不静默去重）
  //   D PutRole(重复 {'docs','docs'})      err=agentteam: 角色 "probe-role" 的插件装配非法: 插件 "docs" 重复声明（显式拒绝，不静默去重）
  // 即 SaveEmployee 与 PutRole（InstantiateRole 的内部入口）两条路都显式拒绝，
  // 所以提交侧保重复不会变成"落盘时被静默吃掉"：错误会经 invoke 抛回、走
  // app.js 的 catch → agentTeamError 内联告警 + toast 回显。
  assert.equal(submitPluginNames("docs; docs").length, 2, "两个重复项原样送后端，前端不减项");
});

test("接线：装配提交路已随 Agent Team 面板退场（app.js 里不留半条死路）", () => {
  // J-1a 前这里是"两条提交路（入库 SaveEmployee / 入职 InstantiateRole）都带上装配"：
  // 断言 app.js 的 agentTeamRolePayload 与 hire 表单 submit 两处都经 withPluginAssembly。
  // 面板退场后这两条路都不存在了 —— 该断言若照旧写就是空壳，所以改成**退场守卫**：
  // 装配的提交侧不得在 app.js 里留下半条接不上的接线。
  assert.doesNotMatch(appSource, /withPluginAssembly/, "app.js 不得再引用随面板退场的装配提交路");
  assert.doesNotMatch(appSource, /agentTeamRolePayload/, "员工入库 / 入职的载荷装配已随面板退场");
  assert.doesNotMatch(appSource, /data-team-hire-plugins/, "hire 表单的装配字段已随面板退场");
  assert.doesNotMatch(appSource, /AgentTeamSaveEmployee|AgentTeamInstantiateRole/, "面板的入库 / 入职 RPC 调用已退场");
  // 反面：插件页自己的来源读数接线照旧（⑧ 钉的就是它）。
  assert.match(appSource, /pluginSourceBadge\(plugin\)/, "插件页的来源读数接线不得被这轮改动带走");
});

// ── 来源读数进面板（wi-frontend，后端键见 commit 118f259）─────────────────────────
//   ④ 来源标签映射；⑤ 缺失不编；⑥ 转义；⑦ local:<根> 的长路径处理；⑧ app.js/index.html 接线。

test("④ 来源标签映射：builtin=随发行包 / vendored=第三方移植 / local=本机自建，且带上来源与载入根", () => {
  const builtin = pluginSourceBadge({
    name: "default", source_kind: "builtin",
    source_url: "https://github.com/RedHuang-0622/seelex", source_root: "/payload/default",
  });
  assert.equal(builtin.hasSource, true);
  assert.equal(builtin.label, "随发行包");
  assert.ok(builtin.note.includes("来源 https://github.com/RedHuang-0622/seelex"), builtin.note);
  assert.ok(builtin.note.includes("载入根 /payload/default"), builtin.note);

  const vendored = pluginSourceBadge({
    name: "impeccable", source_kind: "vendored",
    source_url: "https://github.com/pbakaus/impeccable", source_root: "/payload/impeccable",
  });
  assert.equal(vendored.label, "第三方移植");

  const localRoot = "C:\\Users\\me\\src\\seelex\\plugins";
  const local = pluginSourceBadge({
    name: "frontend", source_kind: "local",
    source_url: "local:" + localRoot, source_root: localRoot + "\\frontend",
  });
  assert.equal(local.label, "本机自建", "local 必须有可见短标签（不能只靠 tooltip）");
  assert.ok(local.note.includes("来源 local:" + localRoot), local.note);
  assert.ok(local.note.includes("载入根 " + localRoot + "\\frontend"), local.note);
});

test("⑤ 缺失来源类型 ≠ 缺失载入根：不编 kind，但载入根照报（不默认成 builtin）", () => {
  // 后端对**未登记**插件**刻意**只下发 source_root——类型两键整键缺席（omitempty），
  // 载入位置必须可读（application/model/state.go 的 source_root；断言见
  // application/core/plugin_source_projection_test.go「未登记插件的载入位置仍必须可读」）。
  // 用户当面问的「这些插件我没在我的 plugins/ 下见过」正是靠这句载入根解释 ⇒
  // 无 kind 时**不得**整块退场（那会让这批行"裸奔"成只有插件名）。
  const unlisted = pluginSourceBadge({ name: "unlisted", source_root: "/local/unlisted" });
  assert.equal(unlisted.hasSource, true, "载入根在场就是一条来源事实");
  assert.equal(unlisted.kind, "");
  assert.equal(unlisted.label, "来源未登记", "类型未登记要可见，但绝不得编成 builtin");
  assert.notEqual(unlisted.label, "随发行包");
  assert.equal(unlisted.note, "载入根 /local/unlisted", "载入根这条事实必须在 note（title）里");
  assert.equal(unlisted.hint, "载入根 /local/unlisted", "也要给可见的第二行");
  // 空串 = 同样缺失类型，但 root 在场 ⇒ 照报。
  const blank = pluginSourceBadge({ name: "unlisted", source_kind: "", source_url: "", source_root: "/x/u" });
  assert.equal(blank.kind, "");
  assert.equal(blank.label, "来源未登记");
  assert.equal(blank.note, "载入根 /x/u");

  // 三条事实（类型 / 地址 / 根）**全缺**才整块退场：只剩插件名，一句都不编。
  for (const plugin of [
    { name: "x" },
    { name: "x", source_kind: null, source_url: null },
    { name: "x", source_kind: "  ", source_url: "", source_root: "" },
  ]) {
    const badge = pluginSourceBadge(plugin);
    assert.equal(badge.hasSource, false, `无任何来源事实不得 hasSource：${JSON.stringify(plugin)}`);
    assert.equal(badge.label, "", "不得编出标签");
    assert.equal(badge.note, "");
    assert.equal(badge.hint, "");
  }
  // 认不得的 kind 既不塞进三档、也不吃掉：原样显形（前端不替后端猜来源）。
  assert.equal(pluginSourceBadge({ name: "x", source_kind: "fork" }).label, "fork");
  assert.notEqual(pluginSourceBadge({ name: "x", source_kind: "fork" }).label, "来源未登记");
  // 只有 kind、没地址没根：标签照显，note/hint 里没有的项不硬凑。
  const bare = pluginSourceBadge({ name: "x", source_kind: "local" });
  assert.equal(bare.label, "本机自建");
  assert.equal(bare.note, "");
  assert.equal(bare.hint, "");
  // 只有地址没根（未登记类型）：类型不编，地址照报。
  const urlOnly = pluginSourceBadge({ name: "x", source_url: "local:/r/p" });
  assert.equal(urlOnly.label, "来源未登记");
  assert.equal(urlOnly.note, "来源 local:/r/p");
  assert.equal(urlOnly.hint, "来源 local:/r/p");
});

test("⑥ 文本一律转义：来源读数进 innerHTML 前先过 escapePluginSourceText（与 components.escapeHtml 同口径）", () => {
  assert.equal(escapePluginSourceText('<img src=x onerror="alert(1)">'),
    "&lt;img src=x onerror=&quot;alert(1)&quot;&gt;");
  assert.equal(escapePluginSourceText("a&b'c"), "a&amp;b&#039;c");
  assert.equal(escapePluginSourceText(null), "");
  assert.equal(escapePluginSourceText(undefined), "");

  // 恶意 / 异常根路径与地址：转义后不得留下可执行的原始尖括号或引号。
  const bad = pluginSourceBadge({
    name: "evil", source_kind: "local",
    source_url: 'local:/tmp/<script>alert(1)</script>"',
    source_root: '/tmp/"><b>x</b>',
  });
  for (const value of [bad.label, bad.note, bad.hint]) {
    const escaped = escapePluginSourceText(value);
    assert.equal(escaped.includes("<"), false, escaped);
    assert.equal(escaped.includes(">"), false, escaped);
    assert.equal(escaped.includes('"'), false, escaped);
  }
  assert.match(escapePluginSourceText(bad.note), /&lt;script&gt;alert\(1\)&lt;\/script&gt;/);
});

test("⑦ local:<根> 的长路径：可见旁注收口、完整路径留在 note（title）", () => {
  const longRoot = "/home/somebody/very/deep/checkout/tree/seelex/plugins";
  assert.equal(shortenSourceUrl("local:" + longRoot), "local:…/seelex/plugins");
  assert.equal(shortenRootPath(longRoot + "/frontend"), "…/plugins/frontend");
  // Windows 根沿用反斜杠分隔符，别被显示成 POSIX 根。
  assert.equal(shortenRootPath("C:\\Users\\me\\plugins\\frontend"), "…\\plugins\\frontend");
  // 短路径原样返回（不做无谓的省略）。
  assert.equal(shortenRootPath("/p/plugins"), "/p/plugins");
  assert.equal(shortenSourceUrl("https://github.com/pbakaus/impeccable"), "https://github.com/pbakaus/impeccable");
  assert.equal(shortenRootPath(""), "");

  const local = pluginSourceBadge({
    name: "frontend", source_kind: "local",
    source_url: "local:" + longRoot, source_root: longRoot + "/frontend",
  });
  assert.ok(local.hint.includes("local:…/seelex/plugins"), local.hint);
  assert.equal(local.hint.includes(longRoot), false, "可见旁注不得拖一长串完整根");
  assert.ok(local.note.includes("local:" + longRoot), "完整 local:<根> 必须留在 note（title）里");
});

test("⑧ 接线：面板每行标来源 + 面板上方那句『当前进程加载到的那一份』", async () => {
  assert.match(appSource, /import \{ escapePluginSourceText, pluginSourceBadge \} from "\.\/plugin-source\.js";/);
  assert.match(appSource, /const source = pluginSourceBadge\(plugin\);/);
  // 来源标签可见 + 三块文本都过转义（label / note(title) / hint）。
  assert.match(appSource, /class="chip" title="\$\{escapePluginSourceText\(source\.note\)\}"/);
  assert.match(appSource, /escapePluginSourceText\(source\.label\)/);
  assert.match(appSource, /escapePluginSourceText\(source\.hint\)/);

  const html = await readFile(new URL("./index.html", import.meta.url), "utf8");
  assert.match(html, /这份列表是当前进程加载到的那一份插件（多根 first-wins）；与仓库 plugins\/ 下的不一定是同一份。/);
  // 面板上方那句要一并说清"未登记只报载入根"（用户困惑的正解），否则行内的旁注仍会被当成噪声。
  assert.match(html, /精选目录里没有登记的那个只报这一句事实，不编来源/);
  // 那句说明必须落在 Plugins 区、列表之上。
  const note = html.indexOf("当前进程加载到的那一份");
  const list = html.indexOf('id="plugin-list"');
  assert.ok(note !== -1 && list !== -1 && note < list, "说明必须在 #plugin-list 之上");
});

// ── 第二份事实的等价用例（P2 收口）────────────────────────────────────────────
// "同一件事写了两遍"的实现，各钉一条**等价**用例：不复述实现，只钉两处**行为相同**。
// 生产代码里两份都得留着（escapePluginSourceText 那条注释说明了为什么不能 import
// components：本模块要能被 node --test 直接 import），所以只能在用例里把两者摆到一起比。
// （原 ⑩ 的对照物 normalizePluginNames 活在 agent-team-view.js 里，随 Agent Team 面板
// 退场一起删除——这条对照没有第二份了，用例一并退场，见文件头注。）

test("⑨ 转义同口径：escapePluginSourceText 与 components.escapeHtml 在同一语料上逐字相等", () => {
  // 语料 = 来源面真实会出现的文本（标签 / 地址 / 根路径）+ 会打穿 innerHTML 的畸形串。
  const corpus = [
    "", "default", "本机自建", "第三方移植", "随发行包",
    "/payload/default", "C:\\Users\\me\\src\\seelex\\plugins",
    "local:C:\\Users\\me\\src\\seelex\\plugins", "…\\plugins\\frontend",
    "https://github.com/pbakaus/impeccable?a=1&b=2", "https://github.com/RedHuang-0622/seelex",
    '<img src=x onerror="alert(1)">', "<script>alert('x')</script>", '/tmp/"><b>x</b>',
    "5 > 3 && 2 < 4", "a&b'c\"d", "&amp;", "&#039;", "<>&\"'",
  ];
  for (const text of corpus) {
    assert.equal(escapePluginSourceText(text), escapeHtml(text), `同一语料必须逐字相等：${JSON.stringify(text)}`);
  }
  // 唯一分歧面（显式钉住，别让它变成"悄悄漂移"或"顺手统一"）：null 字面量。
  //   escapeHtml(null)               = "null"（String(null)）
  //   escapePluginSourceText(null)   = ""（缺失口径：未登记与 null 一视同仁）
  // 展示侧永远只喂字符串（label / note / hint 都由 pluginSourceBadge 先 trim 成串），
  // 所以这条分歧不落在任何渲染路径上；语义不同就该被看见，而不是被抹平。
  assert.equal(escapeHtml(null), "null");
  assert.equal(escapePluginSourceText(null), "");
  assert.deepEqual([escapeHtml(undefined), escapePluginSourceText(undefined)], ["", ""]);
});
