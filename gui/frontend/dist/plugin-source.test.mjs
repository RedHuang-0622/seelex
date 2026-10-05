import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { escapePluginSourceText, pluginSourceBadge, shortenRootPath, shortenSourceUrl, submitPluginNames, withPluginAssembly } from "./plugin-source.js";

// wi-editor 提交侧接线的验收（本轮口径，leader 2026-10-05 裁决）：
//   ① 提交载荷带 plugins；② 空 = 不写该键；③ 重复项**原样提交**，由后端显式拒绝。
// 回读 / 展示侧（agent-team-view.js 的 normalizePluginNames，会去重）不在本轮改动内。
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

test("⑤ 缺失来源面 = 只显示名字：整键缺席 / 空串都不编来源（不默认成 builtin）", () => {
  for (const plugin of [
    { name: "unlisted", source_root: "/local/unlisted" },                      // 未登记：来源两键整缺
    { name: "unlisted", source_kind: "", source_url: "", source_root: "/x/u" }, // 空串 = 同样缺失
    { name: "unlisted", source_kind: null, source_url: null },
  ]) {
    const badge = pluginSourceBadge(plugin);
    assert.equal(badge.hasSource, false, `缺失来源不得 hasSource：${JSON.stringify(plugin)}`);
    assert.equal(badge.label, "", "不得编出标签");
    assert.equal(badge.note, "");
    assert.equal(badge.hint, "");
  }
  // 认不得的 kind 既不塞进三档、也不吃掉：原样显形（前端不替后端猜来源）。
  assert.equal(pluginSourceBadge({ name: "x", source_kind: "fork" }).label, "fork");
  // 只有 kind、没地址没根：标签照显，note/hint 里没有的项不硬凑。
  const bare = pluginSourceBadge({ name: "x", source_kind: "local" });
  assert.equal(bare.label, "本机自建");
  assert.equal(bare.note, "");
  assert.equal(bare.hint, "");
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
  // 那句说明必须落在 Plugins 区、列表之上。
  const note = html.indexOf("当前进程加载到的那一份");
  const list = html.indexOf('id="plugin-list"');
  assert.ok(note !== -1 && list !== -1 && note < list, "说明必须在 #plugin-list 之上");
});
