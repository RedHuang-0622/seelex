// account-admin.test.mjs — 「设置 → 账户」判据层的机械断言。
//
// 钉住三件事：草稿只发该发的字段（可选留空就不发）、必填缺一不提交、余额文案把
// 供应商给的金额字符串**原样**带出来（精度不能被界面改写）。
import assert from "node:assert/strict";
import test from "node:test";

import {
  accountBalanceRows,
  accountDraftFromForm,
  balanceReadingText,
  formatBalanceInfo,
} from "./account-admin.js";

test("草稿：必填缺一不提交，且错误说人话", () => {
  assert.match(accountDraftFromForm({}).error, /角色/);
  assert.match(accountDraftFromForm({ role: "agent" }).error, /模型/);
  assert.match(accountDraftFromForm({ role: "agent", model: "m" }).error, /API Key/);
});

test("草稿：只发填了的字段，可选留空一律不发（后端继承 defaults）", () => {
  const { draft, error } = accountDraftFromForm({
    role: "  agent  ",
    model: " deepseek-flash ",
    apiKey: " sk-abc ",
  });
  assert.equal(error, undefined);
  assert.deepEqual(draft, { role: "agent", model: "deepseek-flash", api_key: "sk-abc" });
  assert.equal("provider" in draft, false);
  assert.equal("base_url" in draft, false);
  assert.equal("reasoning_effort" in draft, false);
});

test("草稿：填了的可选字段要带上，且键名与后端 JSON 契约一致", () => {
  const { draft } = accountDraftFromForm({
    role: "subagent",
    model: "deepseek-flash",
    apiKey: "sk-abc",
    provider: "openai",
    baseUrl: "https://api.deepseek.com",
    reasoningEffort: "low",
  });
  assert.deepEqual(draft, {
    role: "subagent",
    model: "deepseek-flash",
    api_key: "sk-abc",
    provider: "openai",
    base_url: "https://api.deepseek.com",
    reasoning_effort: "low",
  });
});

test("余额文案：可用性、币种与金额原样出现", () => {
  assert.equal(
    formatBalanceInfo({ currency: "CNY", total: "110.00", granted: "10.00", topped_up: "100.00" }),
    "CNY 110.00（赠送 10.00 / 充值 100.00）",
  );
  assert.equal(formatBalanceInfo({ currency: "CNY", total: "110.00" }), "CNY 110.00");

  assert.equal(
    balanceReadingText({
      balance: { available: true, infos: [{ currency: "CNY", total: "110.00" }] },
    }),
    "可用 · CNY 110.00",
  );
  // 额度耗尽：这是 402 之前唯一能提前看到的信号，必须照实说"不可用"。
  assert.equal(
    balanceReadingText({
      balance: { available: false, infos: [{ currency: "CNY", total: "0.00" }] },
    }),
    "不可用 · CNY 0.00",
  );
});

test("余额文案：没查过、查不到、读数空表三种态各有说法", () => {
  assert.equal(balanceReadingText(null), "");
  assert.equal(balanceReadingText({ error: "该供应商没有可查询的余额接口" }), "查不到：该供应商没有可查询的余额接口");
  assert.equal(
    balanceReadingText({ balance: { available: true, infos: [] } }),
    "可用 · 读数里没有币种条目",
  );
});

test("行模型：一行一个账号，带 provider/model 摘要与各自的读数", () => {
  const readings = new Map([["agent-2", { balance: { available: true, infos: [{ currency: "CNY", total: "5.00" }] } }]]);
  const rows = accountBalanceRows(
    [
      { name: "agent-1", provider: "openai", model: "deepseek-flash", role: "agent", api_key_set: true },
      { name: "agent-2", provider: "openai", model: "deepseek-flash", disabled: true, role: "agent", api_key_set: false, fallback: true },
      { name: "" },
    ],
    readings,
  );
  assert.equal(rows.length, 3);
  assert.equal(rows[0].label, "agent-1（openai / deepseek-flash）");
  assert.equal(rows[0].reading, "");
  assert.equal(rows[0].role, "agent");
  assert.equal(rows[0].health, "");
  assert.equal(rows[1].reading, "可用 · CNY 5.00");
  assert.equal(rows[1].disabled, true);
  assert.equal(rows[1].health, "未配置密钥 · 内置兜底");
  // 缺字段时给可读占位，不显示 undefined。
  assert.equal(rows[2].label, "（未标注供应商 / 未标注模型）");
});
