// account-admin.js — 「设置 → 账户」两条链路的判据层：手动添加 与 逐账号查余额。
//
// 这里只放**纯函数**（表单值 → 草稿、读数 → 文案）：DOM 与 invoke 的接线留在
// app.js。分开的理由与 account-window.js / role-settings.js 一致——判据能被
// `node --test` 直接钉住，不用起 GUI。
//
// 口径提醒：本模块**不做**账号档的最终校验（必填、角色词表、api_key 合法性都由
// 后端判，前端先说一句只是为了让错误出现在人手边）。所以这里的错误文案只覆盖
// "明显空着没填"，不复制后端的判据。

// accountDraftFromForm 把表单值整理成 AddAccount 的草稿（`accountDraftFromForm` 的
// 返回形状是 `{ draft }` 或 `{ error }`，调用方必须先看 error）。
//
// 可选字段留空即**不发送**（后端按 accounts.yaml 的 defaults 继承）：发空串与不发
// 在加载器那里等价，但不发能让"这条输入到底写了什么"在 IPC 日志里更清楚。
export function accountDraftFromForm(values) {
  const role = text(values?.role);
  const model = text(values?.model);
  const apiKey = text(values?.apiKey);
  if (!role) return { error: "先选一个账号角色" };
  if (!model) return { error: "模型不能为空（如 deepseek-flash）" };
  if (!apiKey) return { error: "API Key 不能为空" };

  const draft = { role, model, api_key: apiKey };
  const provider = text(values?.provider);
  if (provider) draft.provider = provider;
  const baseUrl = text(values?.baseUrl);
  if (baseUrl) draft.base_url = baseUrl;
  const reasoningEffort = text(values?.reasoningEffort);
  if (reasoningEffort) draft.reasoning_effort = reasoningEffort;
  return { draft };
}

// balanceReadingText 把一行余额读数（成功或失败）翻成一句可读文案。
//
// entry 形状：`{ balance }` / `{ error }` / 空（还没查过）。空返回空串（调用方
// 决定显示"未查询"还是什么都不显示）。
export function balanceReadingText(entry) {
  if (!entry) return "";
  if (entry.error) return `查不到：${text(entry.error)}`;
  const balance = entry.balance;
  if (!balance) return "";
  const availability = balance.available ? "可用" : "不可用";
  const infos = Array.isArray(balance.infos) ? balance.infos : [];
  if (!infos.length) return `${availability} · 读数里没有币种条目`;
  return `${availability} · ${infos.map(formatBalanceInfo).join(" / ")}`;
}

// formatBalanceInfo 是一种币种的读数文案：`CNY 110.00（赠送 10.00 / 充值 100.00）`。
//
// 金额一律按后端给的字符串原样显示（"110.00" 不变成 "110"）：界面对账要对得上，
// 精度归供应商。
export function formatBalanceInfo(info) {
  const money = `${text(info?.currency) || "?"} ${text(info?.total) || "?"}`;
  const parts = [];
  if (text(info?.granted)) parts.push(`赠送 ${text(info.granted)}`);
  if (text(info?.topped_up)) parts.push(`充值 ${text(info.topped_up)}`);
  return parts.length ? `${money}（${parts.join(" / ")}）` : money;
}

// accountBalanceRows 给出一行一个账号的**行模型**（渲染与测试共用一份口径）。
//
// 行必须带 provider/model 摘要：账号名（agent-2）本身不说明它连的是哪家、哪个模型，
// 而"给哪条账号查余额"正是这个面板的语义。
export function accountBalanceRows(accounts, readings = new Map()) {
  const list = Array.isArray(accounts) ? accounts : [];
  return list.map(account => {
    const name = text(account?.name);
    const role = text(account?.role);
    const provider = text(account?.provider) || "未标注供应商";
    const model = text(account?.model) || "未标注模型";
    const entry = readings instanceof Map ? readings.get(name) : readings?.[name];
    const health = [];
    if (account?.api_key_set === false) health.push("未配置密钥");
    if (account?.fallback) health.push("内置兜底");
    return {
      name,
      label: `${name}（${provider} / ${model}）`,
      role,
      health: health.join(" · "),
      fallback: Boolean(account?.fallback),
      reading: balanceReadingText(entry),
      disabled: Boolean(account?.disabled),
    };
  });
}

function text(value) {
  return value === null || value === undefined ? "" : String(value).trim();
}
