// 两轴换肤加载层：**深浅（mode）× 皮肤（skin）**。
//
// 三层分工，别混：
//   1. 组件库（vendor/pico.min.css）= 元素基线与通用组件皮；
//   2. styles.css = Seelex 语义 token + 组件样式，并把 --pico-* 桥接到 token，
//      **同时提供深浅两套中性基座**（`:root` 浅色 / `:root[data-theme="dark"]` 深色）；
//   3. 皮肤包（themes/<skin>.css）= 只覆盖**品牌 token** 的换肤层。
//
// 两条正交的轴：
//   · 深浅（mode）：`<html data-theme="light|dark">`，由 styles.css 的中性基座响应；
//   · 皮肤（skin）：`<link id="seelex-skin">` 指向 themes/<skin>.css 的品牌 token。
// 两者可自由组合（5 皮肤 × 2 深浅），互不耦合。
//
// 本模块只做"选哪套 token"与"切 <html data-theme>"，不产出任何颜色：
// 颜色事实在 styles.css / 皮肤 CSS 里，契约见 themes/README.md。
//
// 安全边界：皮肤路径只允许同源相对路径 `themes/<skin>.css`，id 限
// [a-z0-9-]——`../`、绝对 URL、query/hash 一律拒绝（防路径逃逸/远程注入）。

export const SKIN_STORAGE_KEY = "seelex.skin";
export const MODE_STORAGE_KEY = "seelex.mode";
export const SKIN_LINK_ID = "seelex-skin";
const SKIN_SLUG = /^[a-z0-9][a-z0-9-]{0,31}$/;
const MODE_IDS = new Set(["dark", "light"]);
const DEFAULT_MODE = "light";

// skinID 归一化皮肤 id：非法（含路径分隔、点、空格、超长）返回空串。
export function skinID(raw) {
  const value = String(raw ?? "").trim().toLowerCase();
  return SKIN_SLUG.test(value) ? value : "";
}

// skinHref 由皮肤条目生成皮肤文件路径；没有文件（品牌 token 走 styles.css 缺省）返回空串。
export function skinHref(skin) {
  const id = skinID(skin?.id);
  if (!id) return "";
  const file = String(skin?.file ?? "").trim();
  if (!file) return "";
  if (!file.startsWith("themes/") || file.includes("..") || /[?#]/.test(file)) return "";
  return file;
}

// modeID 归一化深浅 id：只认 light/dark，其它返回空串。
export function modeID(raw) {
  const value = String(raw ?? "").trim().toLowerCase();
  return MODE_IDS.has(value) ? value : "";
}

// normalizeThemeManifest 归一化 themes/manifest.json（schema 2）：
// 丢弃畸形条目、按 id 去重、保证 default_skin / default_mode 指向存在的项。
export function normalizeThemeManifest(raw) {
  const source = raw && typeof raw === "object" ? raw : {};

  const skins = [];
  const seenSkins = new Set();
  for (const entry of Array.isArray(source.skins) ? source.skins : []) {
    if (!entry || typeof entry !== "object") continue;
    const id = skinID(entry.id);
    if (!id || seenSkins.has(id)) continue;
    seenSkins.add(id);
    skins.push({
      id,
      name: typeof entry.name === "string" && entry.name ? entry.name : id,
      description: typeof entry.description === "string" ? entry.description : "",
      swatches: Array.isArray(entry.swatches)
        ? entry.swatches.filter(item => typeof item === "string" && item).slice(0, 6)
        : [],
      file: skinHref({ id, file: entry.file })
    });
  }

  const modes = [];
  const seenModes = new Set();
  for (const entry of Array.isArray(source.modes) ? source.modes : []) {
    const id = modeID(entry?.id);
    if (!id || seenModes.has(id)) continue;
    seenModes.add(id);
    modes.push({
      id,
      name: typeof entry?.name === "string" && entry.name ? entry.name : id,
      description: typeof entry?.description === "string" ? entry.description : ""
    });
  }

  const wantedSkin = skinID(source.default_skin);
  const defaultSkinId = wantedSkin && seenSkins.has(wantedSkin) ? wantedSkin : (skins[0]?.id || "");
  const wantedMode = modeID(source.default_mode);
  const fallbackMode = seenModes.has(DEFAULT_MODE) ? DEFAULT_MODE : (modes[0]?.id || DEFAULT_MODE);
  const defaultModeId = wantedMode && seenModes.has(wantedMode) ? wantedMode : fallbackMode;

  return { schemaVersion: Number(source.schema_version) || 2, defaultSkinId, defaultModeId, skins, modes };
}

export function findSkin(manifest, id) {
  const wanted = skinID(id);
  if (!wanted) return null;
  return manifest.skins.find(skin => skin.id === wanted) || null;
}

// resolveSkin 返回"该用哪套皮肤"：请求的 id → 上次选择 → 默认。
export function resolveSkin(manifest, requested, stored) {
  return findSkin(manifest, requested) || findSkin(manifest, stored) || findSkin(manifest, manifest.defaultSkinId);
}

// resolveMode 返回"该用哪个深浅"：请求 → 上次选择 → 默认。
export function resolveMode(manifest, requested, stored) {
  const wanted = modeID(requested) || modeID(stored);
  if (wanted && manifest.modes.some(mode => mode.id === wanted)) return wanted;
  return manifest.defaultModeId;
}

// createThemeController 用注入的 document/storage 驱动换肤（便于离线单测）。
// onApplied({ skin, mode }) 是"这套外观已生效"的回流口：token 的 JS 消费方（例：终端
// xterm 的配色）只在创建时取一次 token，CSS 变量变了不会自己重取，需要在
// 换肤/切深浅后收到通知再取一次。
export function createThemeController({ document: doc, storage, manifest, onApplied }) {
  const normalized = normalizeThemeManifest(manifest);
  const listeners = typeof onApplied === "function" ? [onApplied] : [];
  let currentSkin = resolveSkin(normalized, "", readStored(SKIN_STORAGE_KEY));
  let currentMode = resolveMode(normalized, "", readStored(MODE_STORAGE_KEY));

  function readStored(key) {
    try {
      return storage?.getItem?.(key) || "";
    } catch {
      return "";
    }
  }

  function writeStored(key, value) {
    try {
      storage?.setItem?.(key, value);
    } catch {
      /* 隐私模式/配额异常：外观仍然生效，只是记不住 */
    }
  }

  // notifyApplied 把「外观已生效」广播给 token 的 JS 消费方。回流方抛错不能
  // 影响换肤本身，逐个吞掉。
  function notifyApplied() {
    const payload = { skin: currentSkin, mode: currentMode };
    for (const listener of listeners) {
      try {
        listener(payload);
      } catch {
        /* 回流方的问题不阻断换肤 */
      }
    }
  }

  function putSkin(skin) {
    if (!skin) return null;
    let link = doc?.getElementById?.(SKIN_LINK_ID);
    const href = skin.file;
    if (!href) {
      // 没有皮肤文件（品牌 token 走 styles.css 缺省）：移除皮肤链接即回到缺省。
      if (link?.remove) link.remove();
    } else if (link) {
      link.setAttribute("href", href);
    } else if (doc?.createElement && doc?.head?.appendChild) {
      link = doc.createElement("link");
      link.id = SKIN_LINK_ID;
      link.rel = "stylesheet";
      link.setAttribute("href", href);
      doc.head.appendChild(link);
    }
    currentSkin = skin;
    // 换肤本身（<link>）已落地；外链皮肤是异步资源，先立即兜底一次，
    // <link> load 完成时再回流一次——那时读到的 token 才是新皮肤的色值。
    notifyApplied();
    if (href && link) link.onload = () => notifyApplied();
    return skin;
  }

  function putMode(mode) {
    if (!mode) return null;
    const root = doc?.documentElement;
    if (root) root.dataset.theme = mode;
    currentMode = mode;
    notifyApplied();
    return mode;
  }

  return {
    skins: normalized.skins,
    modes: normalized.modes,
    defaultSkinId: normalized.defaultSkinId,
    defaultModeId: normalized.defaultModeId,
    applySkin(id) {
      const skin = findSkin(normalized, id) || findSkin(normalized, normalized.defaultSkinId);
      if (!skin) return null;
      writeStored(SKIN_STORAGE_KEY, skin.id);
      return putSkin(skin);
    },
    applyMode(id) {
      const mode = resolveMode(normalized, id, "");
      if (!mode) return null;
      writeStored(MODE_STORAGE_KEY, mode);
      return putMode(mode);
    },
    // apply 把当前（含上次记忆）的皮肤与深浅一次性套上，用于启动。
    apply() {
      this.applySkin(currentSkin?.id);
      this.applyMode(currentMode);
    },
    current() {
      return { skin: currentSkin, mode: currentMode };
    }
  };
}

// loadThemeManifest 读内置清单（同源相对路径，运行期无外网）。
export async function loadThemeManifest(fetchImpl, url = "./themes/manifest.json") {
  try {
    const response = await fetchImpl(url, { cache: "no-store" });
    if (!response?.ok) throw new Error(`HTTP ${response?.status}`);
    return normalizeThemeManifest(await response.json());
  } catch {
    // 清单读不到时退回最小可用集合：默认皮肤与深浅永远存在。
    return normalizeThemeManifest({
      schema_version: 2,
      default_skin: "qoder",
      default_mode: "light",
      skins: [{ id: "qoder", name: "Qoder", file: "" }],
      modes: [{ id: "light", name: "浅色" }, { id: "dark", name: "深色" }]
    });
  }
}
