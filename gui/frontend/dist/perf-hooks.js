// 性能追踪钩子（frontend 侧）：把渲染进程可观测指标与后端快照载荷
// 体积对照，让"谁在吃内存"不用再靠任务管理器猜——DOM 节点数 ↔ JS heap
// ↔ 快照 JSON 体积 ↔ 归档 result_ref 数在同一时间线上可查。
//
// 暴露：
//   window.__seelexPerf = {
//     samples: [{t, renderMs, domNodes, jsHeap, snapshotBytes, convMsgs, convChars, largest, truncated, archived}],
//     markRender(ms), poll(), reset(), badge: HTMLElement
//   }
//
// GUI 每 10s 轮询后端 PerfStats（无内容指标）+ 本地渲染成本采样；采样
// 环有界（60 条）。徽标点击展开最近样本的摘要（console.table）。

const SAMPLE_LIMIT = 60;
const POLL_INTERVAL_MS = 10000;

export function createPerfHooks({ getStats, onError } = {}) {
  const samples = [];
  const badge = document.createElement("span");
  badge.className = "perf-badge";
  badge.title = "性能追踪：渲染进程内存/渲染耗时/快照载荷（点击看最近样本摘要）";
  badge.setAttribute("role", "status");
  badge.textContent = "…";

  function domNodes() {
    return document.querySelectorAll("*").length;
  }

  function jsHeap() {
    const memory = performance?.memory;
    if (!memory || typeof memory.usedJSHeapSize !== "number") return 0;
    return memory.usedJSHeapSize;
  }

  function record(extra = {}) {
    const sample = {
      t: Date.now(),
      renderMs: 0,
      domNodes: domNodes(),
      jsHeap: jsHeap(),
      snapshotBytes: 0,
      convMsgs: 0,
      convChars: 0,
      largest: 0,
      truncated: 0,
      archived: 0,
      ...extra
    };
    samples.push(sample);
    if (samples.length > SAMPLE_LIMIT) samples.shift();
    renderBadge(sample);
    return sample;
  }

  function renderBadge(sample) {
    const heapMB = sample.jsHeap ? (sample.jsHeap / 1048576).toFixed(1) : "?";
    const snapshotKB = sample.snapshotBytes ? (sample.snapshotBytes / 1024).toFixed(0) : "?";
    const truncated = sample.truncated ? ` · 截断 ${sample.truncated}` : "";
    badge.textContent = `渲染 ${sample.domNodes} 节点 · JS ${heapMB}MB · 快照 ${snapshotKB}KB${truncated}`;
  }

  async function poll() {
    if (typeof getStats !== "function") return null;
    try {
      const stats = await getStats();
      const sample = record({
        renderMs: 0,
        snapshotBytes: stats?.snapshot_bytes || 0,
        convMsgs: stats?.conversation_messages || 0,
        convChars: stats?.conversation_chars || 0,
        largest: stats?.largest_message_chars || 0,
        truncated: stats?.truncated_outputs || 0,
        archived: stats?.archived_bytes || 0
      });
      // 大块输出出现时打点一次到控制台（对照前端折叠是否生效）。
      if (sample.largest > 4000 || sample.truncated > 0) {
        console.debug("[perf] 大输出样本:", sample);
      }
      return sample;
    } catch (error) {
      onError?.(error);
      return null;
    }
  }

  function start() {
    record({ renderMs: 0 });
    if (typeof setInterval === "function") {
      setInterval(() => void poll(), POLL_INTERVAL_MS);
    }
    void poll();
  }

  function markRender(ms) {
    const latest = samples.length ? samples[samples.length - 1] : null;
    if (latest && Date.now() - latest.t < 1000) {
      latest.renderMs = Math.max(latest.renderMs, ms);
    } else {
      record({ renderMs: ms });
    }
  }

  function reset() {
    samples.length = 0;
    record({});
  }

  badge.addEventListener("click", () => {
    console.table(samples.map((s, i) => ({
      i,
      t: new Date(s.t).toLocaleTimeString(),
      renderMs: s.renderMs,
      domNodes: s.domNodes,
      jsHeapMB: s.jsHeap ? (s.jsHeap / 1048576).toFixed(1) : "?",
      snapshotKB: s.snapshotBytes ? (s.snapshotBytes / 1024).toFixed(0) : "?",
      convMsgs: s.convMsgs,
      convChars: s.convChars,
      largest: s.largest,
      truncated: s.truncated,
      archivedKB: s.archived ? (s.archived / 1024).toFixed(0) : "?"
    })));
  });

  return { samples, badge, record, markRender, poll, reset, start };
}
