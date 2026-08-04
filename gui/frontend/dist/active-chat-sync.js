// createActiveChatSnapshotSync repairs a missed desktop event while a chat is
// active. Events remain the fast path; this bounded polling path only asks the
// authoritative Bridge Snapshot to reconcile a visibly running chat.
export function createActiveChatSnapshotSync(options) {
  const interval = Number(options.interval) > 0 ? Number(options.interval) : 1000;
  const schedule = options.schedule || setTimeout;
  const cancel = options.cancel || clearTimeout;
  let timer = null;
  let snapshot = null;
  let stopped = false;

  function observe(nextSnapshot) {
    snapshot = nextSnapshot || null;
    if (timer !== null) {
      cancel(timer);
      timer = null;
    }
    if (stopped || !snapshot?.chat?.running) return;
    timer = schedule(refresh, interval);
  }

  async function refresh() {
    timer = null;
    const observed = snapshot;
    try {
      await options.refresh();
    } catch (error) {
      options.onError?.(error);
      observe(observed);
    }
  }

  function stop() {
    stopped = true;
    if (timer !== null) cancel(timer);
    timer = null;
    snapshot = null;
  }

  return { observe, stop };
}
