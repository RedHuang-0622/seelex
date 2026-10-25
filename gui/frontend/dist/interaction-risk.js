// interaction-risk.js 把 Seele 的审批风险枚举翻成明确的中文标签。
//
// 审批里的 low / medium / high 是**操作风险**，不是 composer 的思考强度。
// 直接显示英文 medium 会被误读成 reasoning effort，因此 UI 不原样露出 wire 值。
const APPROVAL_RISK_LABELS = Object.freeze({
  low: "低风险",
  medium: "中风险",
  high: "高风险",
});

export function approvalRiskLabel(risk) {
  const value = String(risk ?? "").trim().toLowerCase();
  return APPROVAL_RISK_LABELS[value] || (value ? `风险：${value}` : "待评估");
}
