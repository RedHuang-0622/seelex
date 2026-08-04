export function createRuntimeEventBinder(options) {
  let bound = false;

  return function bind(runtime) {
    if (bound) return true;
    if (!runtime || typeof runtime.EventsOn !== "function") return false;

    runtime.EventsOn("seelex:event", event => options.client.handleEvent(event));
    runtime.EventsOn("seelex:ready", snapshot => {
      try { options.client.acceptSnapshot(snapshot, "bottom"); }
      catch (error) { options.onError(error); }
    });
    bound = true;
    return true;
  };
}
