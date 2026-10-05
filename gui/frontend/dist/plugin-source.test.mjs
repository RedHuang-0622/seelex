import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { escapeHtml } from "./components.js";
import { hirePanel, normalizePluginNames } from "./agent-team-view.js";
import { escapePluginSourceText, pluginSourceBadge, shortenRootPath, shortenSourceUrl, submitPluginNames, withPluginAssembly } from "./plugin-source.js";

// wi-editor 提交侧接线的验收（leader 2026-10-05 裁决）：
//   ① 提交载荷带 plugins；② 空 = 不写该键；③ 重复项**原样提交**，由后端显式拒绝。
// 回读 / 展示侧（agent-team-view.js 的 normalizePluginNames，会去重）**不做**改动：
// 它只作为 ⑩ 的对照物被 import 进来（两份切分只在"去重"上不同）。
// ⑨/⑪ 同理：只为了把"同一件事写了两遍"的两处行为摆到一起比（见各用例注释）。
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

test("接线：两条提交路（入库 SaveEmployee / 入职 InstantiateRole）都带上装配", () => {
  // 老路：agentTeamRolePayload（"搬运员工"的载荷）必须带装配。
  assert.match(appSource, /return withPluginAssembly\(payload, found\.plugins\);/);
  // 入库按钮与员工库「入职」按钮两条分支都经这份载荷。
  assert.match(appSource, /dataset\.teamEmployeeSave\)\s*\{[\s\S]{0,600}?agentTeamRolePayload\(/);
  assert.match(appSource, /dataset\.teamEmployeeHire\)\s*\{[\s\S]{0,600}?agentTeamRolePayload\(/);

  // 新路：hire 表单 submit 读 data-team-hire-plugins，且**同一份**规整。
  const wire = appSource.indexOf('withPluginAssembly(role, hireForm.querySelector("[data-team-hire-plugins]")');
  assert.notEqual(wire, -1, "submit 必须读 data-team-hire-plugins");
  const saveEmployee = appSource.indexOf('invoke("AgentTeamSaveEmployee", "", role)', wire);
  const instantiate = appSource.indexOf('invoke("AgentTeamInstantiateRole", "", role, 0)', wire);
  assert.notEqual(saveEmployee, -1, "library 作用域（员工库保存）必须在这条接线之后");
  assert.notEqual(instantiate, -1, "会话作用域（入职/修改 = PutRole 那条路）必须在这条接线之后");
  assert.ok(wire < saveEmployee && wire < instantiate);

  // 提交侧不得引入会去重的那份规整（去重只属于回读/展示侧）。
  assert.doesNotMatch(appSource, /normalizePluginNames/);
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
  assert.match(appSource, /import \{ escapePluginSourceText, pluginSourceBadge, withPluginAssembly \} from "\.\/plugin-source\.js";/);
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
// 两条"同一件事写了两遍"的实现，各自钉一条**等价**用例：不复述实现，只钉两处**行为相同**。
// 生产代码里两份都得留着（escapePluginSourceText 那条注释说明了为什么不能 import
// components：本模块要能被 node --test 直接 import），所以只能在用例里把两者摆到一起比。

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

test("⑩ 切分同口径：submitPluginNames 与 normalizePluginNames 只在『去重』上不同", () => {
  // 两份实现（提交侧 / 回读侧）的分隔符与 trim 口径必须逐项相等，**唯一**差别是回读侧
  // 会去重（保序）。同一语料喂两边作断言——任一侧改了分隔符 / 空值口径，这条就会红。
  const corpus = [
    " cad, docs、design ", "cad docs；design;draw", "cad, cad，cad", "docs; docs",
    "cad,，、 ； docs", "   ", "、 ； ", "单一", ",", "a\u00a0b", "a\tb\nc",
    [" cad ", "", "docs"], ["cad", " cad ", "docs"], ["docs", "docs"], [], ["", "   "], [null, 5, "cad"],
    null, undefined, 5, {}, true,
  ];
  for (const input of corpus) {
    const submit = submitPluginNames(input);
    const readback = normalizePluginNames(input);
    assert.deepEqual(readback, submit ? [...new Set(submit)] : null,
      `两边只差去重：${JSON.stringify(input)}`);
  }
  // 反向对照（阴性）：确实有语料能让两者**不相等**，否则上一条断言是空的。
  assert.deepEqual(submitPluginNames("cad, cad"), ["cad", "cad"]);
  assert.deepEqual(normalizePluginNames("cad, cad"), ["cad"]);
  // 空值口径两边同形：空 → null（调用方据此不写该键）。
  for (const empty of ["", "   ", ",", [], null, undefined]) {
    assert.equal(submitPluginNames(empty), null);
    assert.equal(normalizePluginNames(empty), null);
  }
});

// ── 装配上限提示（P1 收口）────────────────────────────────────────────────────
test("⑪ 装配上限提示不复述写死的数字：读不到生效值就只报配置键", () => {
  // 生效上限由配置 limits.plugins.per_teammate 决定（Go 侧 seelexctx.PluginLimits.PerTeammate；
  // 前端不掌握配置，快照 / 看板投影里也没有这一项——它只出现在 team_plan 回执里）。
  // 所以默认提示**只报配置键**：写死的"上限 3 个"在配置抬高之后就是失真提示。
  const panel = hirePanel({ members: [] }, null);
  assert.doesNotMatch(panel, /上限 3 个/, "读不到上限时不得出现写死的数字");
  assert.doesNotMatch(panel, /出厂 3/);
  assert.match(panel, /limits\.plugins\.per_teammate/, "要指向配置键");
  assert.match(panel, /装配（上限以配置为准）/, "可见标签只留「以配置为准」，不编数字");
  assert.match(panel, /上限以配置 limits\.plugins\.per_teammate 为准/, "完整口径在 title 里");
  assert.match(panel, /data-team-hire-plugins/);
  assert.match(panel, /留空 = 不覆盖/, "老口径（留空不提交该键）不许被这轮改动带走");
  // 接线口留着：调用方真读到生效上限就报数字（app.js 拿到读数时传进来即可）。
  assert.match(hirePanel({ members: [] }, null, "session", { pluginLimit: 5 }), /装配（上限 5 个）/);
  // 非法读数（0 / 负数 / 非整数）= 没读到 ⇒ 回到配置键，不编一个 0。
  for (const bogus of [0, -1, "5", 1.5, NaN]) {
    const html = hirePanel({ members: [] }, null, "session", { pluginLimit: bogus });
    assert.doesNotMatch(html, /上限 0 个|上限 -1 个|上限 1\.5 个|上限 NaN 个/, `非法读数 ${String(bogus)}`);
    assert.match(html, /limits\.plugins\.per_teammate/);
  }
});
