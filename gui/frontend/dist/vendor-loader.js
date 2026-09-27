// ── vendor 脚本按需加载 ─────────────────────────────────────
// dist 就是源、没有 bundler，所以重型 vendor 过去一律在 index.html 尾部同步
// 加载：每次启动都为它们付 parse/compile 与内部缓存的内存，哪怕用户从不打开
// 对应预览（PDF.js 368KB + docx-preview 69KB 是其中最大的一块）。
//
// 这里给出一个同源按需装载器：首次真的走到该类型预览时才注入 <script>；
// 同一 src 的并发调用共用同一个 in-flight Promise（不会重复注入）；失败/超时
// 不缓存，下次调用还能重试。
//
// 「已就绪」由调用方给的 isReady 判定（通常查全局对象），所以与"早期仍同步
// 加载"的写法兼容：已就绪直接返回，不注入、不改动既有加载顺序。

const inflight = new Map();

export function ensureVendorScript(src, isReady, options = {}) {
  const doc = options.document || globalThis.document;
  const timeoutMs = options.timeoutMs === undefined ? 20000 : options.timeoutMs;
  if (typeof isReady === "function" && isReady()) return Promise.resolve(true);
  if (!doc || typeof doc.createElement !== "function") {
    return Promise.reject(new Error(`无法注入 vendor 脚本（没有 document）：${src}`));
  }
  const existing = inflight.get(src);
  if (existing) return existing;

  const promise = new Promise((resolve, reject) => {
    const script = doc.createElement("script");
    script.src = src;
    script.async = true;
    let timer = null;
    const settle = (ok, error) => {
      if (timer !== null) clearTimeout(timer);
      inflight.delete(src);
      if (ok) resolve(true);
      else reject(error || new Error(`vendor 脚本加载失败：${src}`));
    };
    script.addEventListener("load", () => settle(true));
    script.addEventListener("error", () => settle(false));
    if (timeoutMs > 0 && typeof setTimeout === "function") {
      timer = setTimeout(() => settle(false, new Error(`vendor 脚本加载超时：${src}`)), timeoutMs);
    }
    const host = doc.head || doc.documentElement;
    if (!host || typeof host.appendChild !== "function") {
      settle(false, new Error(`无法挂载 vendor 脚本（没有 head）：${src}`));
      return;
    }
    host.appendChild(script);
  });
  inflight.set(src, promise);
  return promise;
}

// 诊断用：当前有几个 src 正在加载。
export function vendorScriptsInFlight() {
  return inflight.size;
}
