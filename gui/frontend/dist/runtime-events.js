export function createRuntimeEventBinder(options) {
  let bound = false;

  return function bind(runtime) {
    if (bound) return true;
    if (!runtime || typeof runtime.EventsOn !== "function") return false;

    runtime.EventsOn("seelex:event", event => options.client.handleEvent(event));
    // seelex:ready 是**订阅换代基线**（Bridge 只在 Start / resubscribe 发它）：
    // 新订阅的 delivery_seq 从 1 重计，因此必须走 acceptBaseline 一并复位已应用
    // 水位（同 id 重订阅时只按会话 id 判断是不够的，见 client-state.acceptBaseline）。
    runtime.EventsOn("seelex:ready", snapshot => {
      try { options.client.acceptBaseline(snapshot, "bottom"); }
      catch (error) { options.onError(error); }
    });
    bound = true;
    return true;
  };
}
