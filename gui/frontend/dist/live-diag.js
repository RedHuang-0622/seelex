// live-diag.js ── 会话内容新鲜度诊断角标
//
// 定位“同一视图会话内容不及时”：事件到达、增量应用、整份刷新、缺口补取、
// delta 缓冲都可能是瓶颈。本模块只展示计数与最近水位，点击展开明细；
// 不参与任何业务状态（纯诊断，无后端依赖）。

export function createLiveDiag() {
  const badge = document.createElement("span");
  badge.className = "live-diag-badge";
  badge.setAttribute("role", "status");
  badge.title = "会话新鲜度诊断：ev=事件 inc=增量 刷=整份刷新 gap=缺口 rp=补取 buf=缓冲 delta seq=水位（点击看明细）";
  badge.textContent = "…";
  let latest = null;

  function label(stats) {
    return `ev ${stats.events} · inc ${stats.incrementals} · 刷 ${stats.refreshes}` +
      (stats.gaps ? ` · gap ${stats.gaps}` : "") +
      (stats.replays ? ` · rp ${stats.replays}` : "") +
      (stats.buffered ? ` · buf ${stats.buffered}` : "") +
      ` · seq ${stats.lastSeq}`;
  }

  badge.addEventListener("click", () => {
    if (!latest) return;
    console.debug("[live-diag]", latest);
  });

  return {
    badge,
    update(stats) {
      latest = { ...stats, at: Date.now() };
      badge.textContent = label(latest);
    }
  };
}
