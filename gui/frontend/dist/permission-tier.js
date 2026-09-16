// permission-tier.js — 主会话权限档位（会话粒度）的**纯解析 / 展示口径**。
//
// 从 app.js 抽出来的原因：档位列表与 composer 芯片的数据来源是"后端下发的档位目录 +
// 本会话生效档位"，这两件事都是纯函数（输入 runtime 投影，输出短名/选项/选中态）。
// 抽成模块才能用 `node --test` 直接钉住（app.js 是巨型 DOM 脚本，没法 import）。
// app.js 只保留 DOM 写入：它 import 这里的函数，自己负责 escapeHtml / icon / 元素查找。
//
// 口径（与后端 dto.PermissionTiers 对齐，前端不复制档位名）：
//   1. 目录唯一来源是 runtime.permission_tiers（后端下发），前端不做兜底枚举；
//   2. 本会话生效档位以 runtime.permission_tier 为准，缺失时按旧二元 full_access 回退；
//   3. 目录缺失（后端未下发/为空）时芯片退化成"只显示当前档 id"，而不是编造档位名。

// permissionTierCatalog 取后端下发的档位目录（升序 = 自动度递增）。非数组/无 id 的
// 条目一律丢弃：档位 id 是唯一事实键，缺 id 的条目渲染出来也点不动。
export function permissionTierCatalog(runtime) {
  const tiers = Array.isArray(runtime?.permission_tiers) ? runtime.permission_tiers : [];
  return tiers.filter(tier => tier && tier.id);
}

// permissionTierEntry 在目录里找档位；找不到就只保留 id（旧后端/乐观回执早于目录
// 刷新时仍有当前档可渲染）。刻意不在这里编造 label/short：展示短名统一由
// permissionTierChip 的兜底规则给出，避免出现"目录一份、兜底一份"的漂移。
export function permissionTierEntry(catalog, id) {
  const found = catalog.find(tier => tier.id === id);
  return found || { id, description: "" };
}

// currentPermissionTier 解析本会话生效档位（permission_tier 为准；缺失时按旧二元
// full_access 回退）。
export function currentPermissionTier(runtime) {
  const catalog = permissionTierCatalog(runtime);
  const id = runtime?.permission_tier || (Boolean(runtime?.full_access) ? "full" : "manual");
  return permissionTierEntry(catalog, id);
}

// permissionTierChip 给出芯片模型：{id, short, isFull}。full 档额外标 isFull（芯片要缀
// ✓，也是"本会话免审"的唯一视觉提示）。
export function permissionTierChip(catalog, id) {
  const entry = permissionTierEntry(catalog, id);
  const short = entry.short || entry.label || (entry.id === "full" ? "全权" : entry.id);
  return { id: entry.id, short, isFull: entry.id === "full" };
}

// permissionTierChipForRuntime = 从 runtime 投影直接得到芯片模型。
export function permissionTierChipForRuntime(runtime) {
  const catalog = permissionTierCatalog(runtime);
  return permissionTierChip(catalog, currentPermissionTier(runtime).id);
}

// permissionTierOptions 给出档位列表选项（顺序 = 后端目录顺序；active 标记本会话生效
// 档位）。列表是权威选择入口，因此每一项都带 label/short/description。
export function permissionTierOptions(runtime) {
  const catalog = permissionTierCatalog(runtime);
  const current = currentPermissionTier(runtime);
  return catalog.map(item => ({
    id: item.id,
    label: item.label || item.id,
    short: item.short || item.label || item.id,
    description: item.description || "",
    active: item.id === current.id,
  }));
}

// permissionTierMenuItems = **芯片下拉**的条目模型。它与 permissionTierOptions 同源：
// 同一份后端目录、同一份当前档，所以"点开下拉切档"与"运行状态弹窗里切档"永远显示
// 同一组档位与同一选中态，不会各记一份。isFull 只影响展示（全权档缀 ✓）。
export function permissionTierMenuItems(runtime) {
  return permissionTierOptions(runtime).map(item => Object.assign({}, item, { isFull: item.id === "full" }));
}

// nextTierIndex 是下拉键盘导航的纯函数：↑/↓ 在档位间**循环**。
//   - 没有可选条目（length <= 0）返回 -1：调用方据此不做任何聚焦；
//   - 还没有聚焦项（index < 0）时，↓ 落到第一条、↑ 落到最后一条；
//   - 从两端继续按方向回绕（末条 ↓ = 第一条，首条 ↑ = 末条）。
export function nextTierIndex(length, index, delta) {
  const count = Number(length);
  if (!Number.isFinite(count) || count <= 0) return -1;
  const step = Number(delta) || 0;
  const from = Number(index);
  const base = Number.isFinite(from) && from >= 0 ? from : (step > 0 ? -1 : 0);
  return ((base + step) % count + count) % count;
}
