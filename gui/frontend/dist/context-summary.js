import { escapeHtml } from "./components.js";

const reasonLabel = {
  context_budget: "Context budget",
  large_tool_output: "Large tool output"
};

// Renders only public compaction metadata. Private checkpoints and original
// conversation or tool contents are deliberately not part of this API.
export function renderContextCompactions(compactions = []) {
  if (!Array.isArray(compactions) || compactions.length === 0) return "";
  return `<div class="context-summary-title">Context compression</div>${compactions.map(compaction => {
    const version = Number(compaction?.version || 0);
    const reason = reasonLabel[compaction?.reason] || "Context compression";
    const messages = Number(compaction?.messages_before || 0);
    const tokens = Number(compaction?.estimated_tokens || 0);
    const time = formatTime(compaction?.compacted_at);
    const details = [messages ? `${messages} messages` : "", tokens ? `~${formatNumber(tokens)} tokens` : "", time].filter(Boolean).join(" · ");
    return `<article class="context-summary-item"><header><strong>#${escapeHtml(version || "?")}</strong><span>${escapeHtml(reason)}</span></header><small>${escapeHtml(details)}</small><p>Task checkpoint retained; details can be re-read when needed.</p></article>`;
  }).join("")}`;
}

function formatNumber(value) {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(value);
}

function formatTime(value) {
  const time = new Date(value);
  if (Number.isNaN(time.getTime())) return "";
  return time.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}
