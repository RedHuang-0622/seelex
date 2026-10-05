import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { submitPluginNames, withPluginAssembly } from "./plugin-source.js";

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
