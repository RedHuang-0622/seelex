import { applyEvent, validateSnapshot } from "./protocol.js";

export function createGUIClient(options) {
  let snapshot = null;
  let snapshotRevisionFloor = 0;
  let lastEventSeq = 0;
  let refreshPromise = null;
  let refreshQueued = false;
  let refreshScroll = "auto";

  async function refresh(request = {}) {
    refreshQueued = true;
    refreshScroll = mergeScrollMode(refreshScroll, requestedScrollMode(request));
    if (refreshPromise) return refreshPromise;
    refreshPromise = runRefreshLoop();
    try { await refreshPromise; }
    finally { refreshPromise = null; }
  }

  async function runRefreshLoop() {
    while (refreshQueued) {
      refreshQueued = false;
      const scrollMode = refreshScroll;
      refreshScroll = "auto";
      try {
        acceptSnapshot(await options.loadSnapshot(), scrollMode);
      } catch (error) { options.onError(error); }
    }
  }

  function acceptSnapshot(value, scrollMode = "bottom") {
    const candidate = validateSnapshot(value);
    if (snapshot && Number(candidate.revision) < Number(snapshot.revision || 0)) return false;
    snapshot = candidate;
    snapshotRevisionFloor = Number(candidate.revision || 0);
    options.onSnapshot(snapshot, { scrollMode });
    return true;
  }

  async function handleEvent(event) {
    const result = applyEvent(snapshot, event, lastEventSeq, snapshotRevisionFloor);
    lastEventSeq = result.lastSeq;
    if (result.error) {
      options.onError(result.error);
      return;
    }
    if (result.needsRefresh) {
      await refresh({ scroll: "auto" });
      return;
    }
    snapshot = result.snapshot;
    if (result.changed) options.onIncremental(snapshot, result.changed);
  }

  return { refresh, handleEvent, acceptSnapshot, current: () => snapshot };
}

function requestedScrollMode(options) {
  if (options.scroll === false) return "preserve";
  return typeof options.scroll === "string" ? options.scroll : "auto";
}

function mergeScrollMode(current, next) {
  const priority = { auto: 0, preserve: 1, anchor: 2, bottom: 3 };
  return priority[next] > priority[current] ? next : current;
}
