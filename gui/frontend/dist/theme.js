// 皮肤（材质包）加载层。
//
// 三层分工，别混：
//   1. 组件库（vendor/pico.min.css）= 元素基线与通用组件皮；
//   2. styles.css = Seelex 语义 token + 组件样式，并把 --pico-* 桥接到 token；
//   3. 皮肤包（themes/<id>.css）= 只覆盖语义 token 的换肤层。
//
// 本模块只做"选哪套 token"与"切 <html data-theme>"，不产出任何颜色：
// 颜色事实在皮肤 CSS 里，契约见 themes/README.md。
//
// 安全边界：皮肤路径只允许同源相对路径 `themes/<id>.css`，id 限
// [a-z0-9-]——`../`、绝对 URL、query/hash 一律拒绝（防路径逃逸/远程注入）。

export const THEME_STORAGE_KEY = "seelex.theme";
export const THEME_LINK_ID = "seelex-theme";
const THEME_ID = /^[a-z0-9][a-z0-9-]{0,31}$/;
const THEME_MODES = new Set(["dark", "light"]);

// themeID 归一化皮肤 id：非法（含路径分隔、点、空格、大写以外的字符）返回空串。
export function themeID(raw) {
  const value = String(raw ?? "").trim().toLowerCase();
  return THEME_ID.test(value) ? value : "";
}

// themeHref 由 id 生成皮肤文件路径；默认皮肤（file 为空）返回空串。
export function themeHref(theme) {
  const id = themeID(theme?.id);
  if (!id) return "";
  const file = String(theme?.file ?? "").trim();
  if (!file) return "";
  if (!file.startsWith("themes/") || file.includes("..") || /[?#]/.test(file)) return "";
  return file;
}

export function themeMode(theme) {
  const mode = String(theme?.mode ?? "").trim().toLowerCase();
  return THEME_MODES.has(mode) ? mode : "dark";
}

// normalizeThemeManifest 归一化 themes/manifest.json：丢弃畸形条目、按 id 去重、
// 保证 default 指向存在的皮肤（否则退回第一条）。
export function normalizeThemeManifest(raw) {
  const source = raw && typeof raw === "object" ? raw : {};
  const list = Array.isArray(source.themes) ? source.themes : [];
  const seen = new Set();
  const themes = [];
  for (const entry of list) {
    if (!entry || typeof entry !== "object") continue;
    const id = themeID(entry.id);
    if (!id || seen.has(id)) continue;
    seen.add(id);
    themes.push({
      id,
      name: typeof entry.name === "string" && entry.name ? entry.name : id,
      mode: themeMode(entry),
      description: typeof entry.description === "string" ? entry.description : "",
      swatches: Array.isArray(entry.swatches)
        ? entry.swatches.filter(item => typeof item === "string" && item).slice(0, 6)
        : [],
      file: themeHref({ id, file: entry.file })
    });
  }
  const fallback = themes.length ? themes[0].id : "";
  const wanted = themeID(source.default);
  const defaultId = wanted && seen.has(wanted) ? wanted : fallback;
  return { schemaVersion: Number(source.schema_version) || 1, defaultId, themes };
}

export function findTheme(manifest, id) {
  const wanted = themeID(id);
  if (!wanted) return null;
  return manifest.themes.find(theme => theme.id === wanted) || null;
}

// resolveTheme 返回"该用哪套皮肤"：请求的 id → 上次选择 → 默认。
export function resolveTheme(manifest, requested, stored) {
  return findTheme(manifest, requested) || findTheme(manifest, stored) || findTheme(manifest, manifest.defaultId);
}

// createThemeController 用注入的 document/storage 驱动换肤（便于离线单测）。
export function createThemeController({ document: doc, storage, manifest }) {
  const normalized = normalizeThemeManifest(manifest);
  let current = resolveTheme(normalized, "", readStored());

  function readStored() {
    try {
      return storage?.getItem?.(THEME_STORAGE_KEY) || "";
    } catch {
      return "";
    }
  }

  function writeStored(id) {
    try {
      storage?.setItem?.(THEME_STORAGE_KEY, id);
    } catch {
      /* 隐私模式/配额异常：皮肤仍然生效，只是记不住 */
    }
  }

  function apply(theme) {
    if (!theme) return null;
    const root = doc?.documentElement;
    if (root) root.dataset.theme = theme.mode;
    let link = doc?.getElementById?.(THEME_LINK_ID);
    const href = theme.file;
    if (!href) {
      // 默认皮肤：token 就在 styles.css 里，移除皮肤链接即回到默认。
      if (link?.remove) link.remove();
    } else if (link) {
      link.setAttribute("href", href);
    } else if (doc?.createElement && doc?.head?.appendChild) {
      link = doc.createElement("link");
      link.id = THEME_LINK_ID;
      link.rel = "stylesheet";
      link.setAttribute("href", href);
      doc.head.appendChild(link);
    }
    current = theme;
    return theme;
  }

  return {
    themes: normalized.themes,
    defaultId: normalized.defaultId,
    apply(id) {
      const theme = findTheme(normalized, id) || findTheme(normalized, normalized.defaultId);
      if (!theme) return null;
      writeStored(theme.id);
      return apply(theme);
    },
    current() {
      return current;
    }
  };
}

// loadThemeManifest 读内置皮肤清单（同源相对路径，运行期无外网）。
export async function loadThemeManifest(fetchImpl, url = "./themes/manifest.json") {
  try {
    const response = await fetchImpl(url, { cache: "no-store" });
    if (!response?.ok) throw new Error(`HTTP ${response?.status}`);
    return normalizeThemeManifest(await response.json());
  } catch {
    // 清单读不到时退回最小可用集合：默认皮肤永远存在。
    return normalizeThemeManifest({
      schema_version: 1,
      default: "graphite",
      themes: [{ id: "graphite", name: "石墨黄铜", mode: "dark", file: "" }]
    });
  }
}
