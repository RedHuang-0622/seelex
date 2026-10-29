import { renderMarkdown } from "./markdown.js";

const ICONS = {
  command: '<path d="M4 6h16M4 12h10M4 18h7"/><path d="m17 16 3 2-3 2"/>',
  runtime: '<path d="M4 7h10M18 7h2M4 17h2M10 17h10"/><circle cx="16" cy="7" r="2"/><circle cx="8" cy="17" r="2"/>',
  settings: '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .34 1.88l.06.06-2.12 2.12-.06-.06a1.7 1.7 0 0 0-1.88-.34 1.7 1.7 0 0 0-1.04 1.56V20.3h-3v-.08A1.7 1.7 0 0 0 10.66 18.66a1.7 1.7 0 0 0-1.88.34l-.06.06-2.12-2.12.06-.06A1.7 1.7 0 0 0 7 15a1.7 1.7 0 0 0-1.56-1.04h-.08v-3h.08A1.7 1.7 0 0 0 7 9.92a1.7 1.7 0 0 0-.34-1.88L6.6 7.98l2.12-2.12.06.06a1.7 1.7 0 0 0 1.88.34A1.7 1.7 0 0 0 11.7 4.7v-.08h3v.08a1.7 1.7 0 0 0 1.04 1.56 1.7 1.7 0 0 0 1.88-.34l.06-.06 2.12 2.12-.06.06a1.7 1.7 0 0 0-.34 1.88 1.7 1.7 0 0 0 1.56 1.04h.08v3h-.08A1.7 1.7 0 0 0 19.4 15Z"/>',
  send: '<path d="m5 12 14-7-4 14-3-6-7-1Z"/><path d="m12 13 7-8"/>',
  stop: '<rect x="7" y="7" width="10" height="10" rx="1"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  close: '<path d="m6 6 12 12M18 6 6 18"/>',
  terminal: '<path d="m5 7 4 4-4 4M11 17h8"/>',
  copy: '<rect x="8" y="8" width="11" height="11" rx="2"/><path d="M16 8V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h3"/>',
  edit: '<path d="M4 20h4L19 9l-4-4L4 16z"/><path d="m13.5 6.5 4 4"/>',
  expand: '<path d="m8 3-5 5M3 3v5h5M16 3l5 5M21 3v5h-5M8 21l-5-5M3 21v-5h5M16 21l5-5M21 21v-5h-5"/>',
  file: '<path d="M6 3h8l4 4v14H6z"/><path d="M14 3v5h5M9 13h6M9 17h5"/>',
  folder: '<path d="M3 6h7l2 2h9v11H3z"/>',
  source: '<circle cx="12" cy="12" r="3"/><path d="M12 3v3M12 18v3M3 12h3M18 12h3M5.6 5.6l2.1 2.1M16.3 16.3l2.1 2.1M18.4 5.6l-2.1 2.1M7.7 16.3l-2.1 2.1"/>',
  message: '<path d="M4 5h16v12H8l-4 4z"/>',
  skill: '<path d="M12 3 4 7v10l8 4 8-4V7z"/><path d="m4 7 8 4 8-4M12 11v10"/>',
  plugin: '<path d="M8 3v5H3v8h5v5h8v-5h5V8h-5V3z"/>',
  team: '<circle cx="9" cy="8" r="3"/><path d="M3 19a6 6 0 0 1 12 0"/><path d="M16 6.2a3 3 0 0 1 0 5.6M20 19a6 6 0 0 0-1.6-4.1"/>',
  search: '<circle cx="11" cy="11" r="7"/><path d="m21 21-4.3-4.3"/>',
  table: '<rect x="3" y="3" width="18" height="18" rx="2"/><path d="M12 3v18M3 12h18"/>',
  check: '<path d="m5 12 4 4L19 6"/>',
  error: '<circle cx="12" cy="12" r="9"/><path d="M12 7v6M12 17h.01"/>',
  grip: '<circle cx="9" cy="5" r="1"/><circle cx="15" cy="5" r="1"/><circle cx="9" cy="12" r="1"/><circle cx="15" cy="12" r="1"/><circle cx="9" cy="19" r="1"/><circle cx="15" cy="19" r="1"/>',
  "arrow-up": '<path d="M12 19V5M5 12l7-7 7 7"/>',
  "arrow-down": '<path d="M12 5v14M5 12l7 7 7-7"/>',
  recall: '<path d="M9 14 4 9l5-5"/><path d="M4 9h10a6 6 0 0 1 0 12h-3"/>',
  "corner-down-right": '<path d="M4 4v7a4 4 0 0 0 4 4h12"/><path d="m15 10 5 5-5 5"/>',
  refresh: '<path d="M20 11a8 8 0 1 0-2.4 5.7"/><path d="M20 5v6h-6"/>',
  branch: '<circle cx="6" cy="6" r="2.5"/><circle cx="6" cy="18" r="2.5"/><circle cx="18" cy="8" r="2.5"/><path d="M6 8.5v7M8.5 6h4a5.5 5.5 0 0 1 3 5v-0.5a5.5 5.5 0 0 1-3 5h-4"/>',
  "chevron-left": '<path d="m14.5 6-6 6 6 6"/>',
  "chevron-right": '<path d="m9.5 6 6 6-6 6"/>',
  "chevron-up": '<path d="m6 14.5 6-6 6 6"/>',
  "chevron-down": '<path d="m6 9.5 6 6 6-6"/>',
  circle: '<circle cx="12" cy="12" r="8"/>',
  dot: '<circle cx="12" cy="12" r="4.5" fill="currentColor" stroke="none"/>',
  more: '<circle cx="5" cy="12" r="1.6" fill="currentColor" stroke="none"/><circle cx="12" cy="12" r="1.6" fill="currentColor" stroke="none"/><circle cx="19" cy="12" r="1.6" fill="currentColor" stroke="none"/>',
  // grip：拖拽把手（两列竖点，队列条排序用）。`more` 是横三点，做成把手会被读成
  // "更多操作"，所以另立一枚。
  grip: '<circle cx="9" cy="6" r="1.5" fill="currentColor" stroke="none"/><circle cx="15" cy="6" r="1.5" fill="currentColor" stroke="none"/><circle cx="9" cy="12" r="1.5" fill="currentColor" stroke="none"/><circle cx="15" cy="12" r="1.5" fill="currentColor" stroke="none"/><circle cx="9" cy="18" r="1.5" fill="currentColor" stroke="none"/><circle cx="15" cy="18" r="1.5" fill="currentColor" stroke="none"/>',
  star: '<path d="m12 4 2.5 5.1 5.5.8-4 3.9.95 5.5L12 16.7l-4.95 2.6.95-5.5-4-3.9 5.5-.8z" fill="currentColor" stroke="none"/>',
  "star-outline": '<path d="m12 4 2.5 5.1 5.5.8-4 3.9.95 5.5L12 16.7l-4.95 2.6.95-5.5-4-3.9 5.5-.8z"/>',
  user: '<circle cx="12" cy="8" r="3.5"/><path d="M5 20a7 7 0 0 1 14 0"/>',
  layer: '<path d="m12 3 9 5-9 5-9-5z"/><path d="m3 13 9 5 9-5"/>'
};

// Google Material Symbols Rounded 的离线路径子集。运行期没有网络，图标不引 CDN；
// 这里只登记团队看板实际使用的语义图标。路径数据来自 google/material-design-icons
// （Apache-2.0），viewBox 使用上游的 0 -960 960 960。
const MATERIAL_ICONS = {
  timeline: '<path d="M120-240q-33 0-56.5-23.5T40-320q0-33 23.5-56.5T120-400h10.5q4.5 0 9.5 2l182-182q-2-5-2-9.5V-600q0-33 23.5-56.5T400-680q33 0 56.5 23.5T480-600q0 2-2 20l102 102q5-2 9.5-2h21q4.5 0 9.5 2l142-142q-2-5-2-9.5V-640q0-33 23.5-56.5T840-720q33 0 56.5 23.5T920-640q0 33-23.5 56.5T840-560h-10.5q-4.5 0-9.5-2L678-420q2 5 2 9.5v10.5q0 33-23.5 56.5T600-320q-33 0-56.5-23.5T520-400v-10.5q0-4.5 2-9.5L420-522q-5 2-9.5 2H400q-2 0-20-2L198-340q2 5 2 9.5v10.5q0 33-23.5 56.5T120-240Z"/>',
  account_tree: '<path d="M600-200v-40h-80q-33 0-56.5-23.5T440-320v-320h-80v40q0 33-23.5 56.5T280-520H160q-33 0-56.5-23.5T80-600v-160q0-33 23.5-56.5T160-840h120q33 0 56.5 23.5T360-760v40h240v-40q0-33 23.5-56.5T680-840h120q33 0 56.5 23.5T880-760v160q0 33-23.5 56.5T800-520H680q-33 0-56.5-23.5T600-600v-40h-80v320h80v-40q0-33 23.5-56.5T680-440h120q33 0 56.5 23.5T880-360v160q0 33-23.5 56.5T800-120H680q-33 0-56.5-23.5T600-200ZM160-760v160-160Zm520 400v160-160Zm0-400v160-160Zm0 160h120v-160H680v160Zm0 400h120v-160H680v160ZM160-600h120v-160H160v160Z"/>',
  lock: '<path d="M240-80q-33 0-56.5-23.5T160-160v-400q0-33 23.5-56.5T240-640h40v-80q0-83 58.5-141.5T480-920q83 0 141.5 58.5T680-720v80h40q33 0 56.5 23.5T800-560v400q0 33-23.5 56.5T720-80H240Zm0-80h480v-400H240v400Zm240-120q33 0 56.5-23.5T560-360q0-33-23.5-56.5T480-440q-33 0-56.5 23.5T400-360q0 33 23.5 56.5T480-280ZM360-640h240v-80q0-50-35-85t-85-35q-50 0-85 35t-35 85v80ZM240-160v-400 400Z"/>',
  lock_open: '<path d="M240-80q-33 0-56.5-23.5T160-160v-400q0-33 23.5-56.5T240-640h360v-80q0-50-35-85t-85-35q-42 0-73.5 25.5T364-751q-4 14-16.5 22.5T320-720q-17 0-28.5-11t-8.5-26q14-69 69-116t128-47q83 0 141.5 58.5T680-720v80h40q33 0 56.5 23.5T800-560v400q0 33-23.5 56.5T720-80H240Zm0-80h480v-400H240v400Zm240-120q33 0 56.5-23.5T560-360q0-33-23.5-56.5T480-440q-33 0-56.5 23.5T400-360q0 33 23.5 56.5T480-280ZM240-160v-400 400Z"/>',
  task_alt: '<path d="M480-80q-83 0-156-31.5T197-197q-54-54-85.5-127T80-480q0-83 31.5-156T197-763q54-54 127-85.5T480-880q48 0 93.5 11t87.5 32q15 8 19.5 24t-5.5 30q-10 14-26.5 18t-32.5-4q-32-15-66.5-23t-69.5-8q-133 0-226.5 93.5T160-480q0 133 93.5 226.5T480-160q133 0 226.5-93.5T800-480q0-8-.5-15.5T798-511q-2-17 6.5-32.5T830-564q16-5 30 3t16 24q2 14 3 28t1 29q0 83-31.5 156T763-197q-54 54-127 85.5T480-80Zm-56-328 372-373q11-11 27.5-11.5T852-781q11 11 11 28t-11 28L452-324q-12 12-28 12t-28-12L282-438q-11-11-11-28t11-28q11-11 28-11t28 11l86 86Z"/>',
  pending_actions: '<path d="M680-80q-83 0-141.5-58.5T480-280q0-83 58.5-141.5T680-480q83 0 141.5 58.5T880-280q0 83-58.5 141.5T680-80Zm20-208v-92q0-8-6-14t-14-6q-8 0-14 6t-6 14v91q0 8 3 15.5t9 13.5l61 61q6 6 14 6t14-6q6-6 6-14t-6-14l-61-61ZM200-120q-33 0-56.5-23.5T120-200v-560q0-33 23.5-56.5T200-840h167q11-35 43-57.5t70-22.5q40 0 71.5 22.5T594-840h166q33 0 56.5 23.5T840-760v160q0 17-11.5 28.5T800-560q-17 0-28.5-11.5T760-600v-160h-80v80q0 17-11.5 28.5T640-640H320q-17 0-28.5-11.5T280-680v-80h-80v560h180q17 0 28.5 11.5T420-160q0 17-11.5 28.5T380-120H200Zm280-640q17 0 28.5-11.5T520-800q0-17-11.5-28.5T480-840q-17 0-28.5 11.5T440-800q0 17 11.5 28.5T480-760Z"/>',
  schedule: '<path d="M520-496v-144q0-17-11.5-28.5T480-680q-17 0-28.5 11.5T440-640v159q0 8 3 15.5t9 13.5l132 132q11 11 28 11t28-11q11-11 11-28t-11-28L520-496ZM480-80q-83 0-156-31.5T197-197q-54-54-85.5-127T80-480q0-83 31.5-156T197-763q54-54 127-85.5T480-880q83 0 156 31.5T763-763q54 54 85.5 127T880-480q0 83-31.5 156T763-197q-54 54-127 85.5T480-80Zm0-400Zm0 320q133 0 226.5-93.5T800-480q0-133-93.5-226.5T480-800q-133 0-226.5 93.5T160-480q0 133 93.5 226.5T480-160Z"/>',
  history: '<path d="M480-120q-126 0-223-76.5T131-392q-4-15 6-27.5t27-14.5q16-2 29 6t18 24q24 90 99 147t170 57q117 0 198.5-81.5T760-480q0-117-81.5-198.5T480-760q-69 0-129 32t-101 88h70q17 0 28.5 11.5T360-600q0 17-11.5 28.5T320-560H160q-17 0-28.5-11.5T120-600v-160q0-17 11.5-28.5T160-800q17 0 28.5 11.5T200-760v54q51-64 124.5-99T480-840q75 0 140.5 28.5t114 77q48.5 48.5 77 114T840-480q0 75-28.5 140.5t-77 114q-48.5 48.5-114 77T480-120Zm40-376 100 100q11 11 11 28t-11 28q-11 11-28 11t-28-11L452-452q-6-6-9-13.5t-3-15.5v-159q0-17 11.5-28.5T480-680q17 0 28.5 11.5T520-640v144Z"/>',
  group: '<path d="M40-272q0-34 17.5-62.5T104-378q62-31 126-46.5T360-440q66 0 130 15.5T616-378q29 15 46.5 43.5T680-272v32q0 33-23.5 56.5T600-160H120q-33 0-56.5-23.5T40-240v-32Zm800 112H738q11-18 16.5-38.5T760-240v-40q0-44-24.5-84.5T666-434q51 6 96 20.5t84 35.5q36 20 55 44.5t19 53.5v40q0 33-23.5 56.5T840-160ZM360-480q-66 0-113-47t-47-113q0-66 47-113t113-47q66 0 113 47t47 113q0 66-47 113t-113 47Zm400-160q0 66-47 113t-113 47q-11 0-28-2.5t-28-5.5q27-32 41.5-71t14.5-81q0-42-14.5-81T544-792q14-5 28-6.5t28-1.5q66 0 113 47t47 113ZM120-240h480v-32q0-11-5.5-20T580-306q-54-27-109-40.5T360-360q-56 0-111 13.5T140-306q-9 5-14.5 14t-5.5 20v32Zm240-320q33 0 56.5-23.5T440-640q0-33-23.5-56.5T360-720q-33 0-56.5 23.5T280-640q0 33 23.5 56.5T360-560Zm0 320Zm0-400Z"/>',
  person: '<path d="M480-480q-66 0-113-47t-47-113q0-66 47-113t113-47q66 0 113 47t47 113q0 66-47 113t-113 47ZM160-240v-32q0-34 17.5-62.5T224-378q62-31 126-46.5T480-440q66 0 130 15.5T736-378q29 15 46.5 43.5T800-272v32q0 33-23.5 56.5T720-160H240q-33 0-56.5-23.5T160-240Zm80 0h480v-32q0-11-5.5-20T700-306q-54-27-109-40.5T480-360q-56 0-111 13.5T260-306q-9 5-14.5 14t-5.5 20v32Zm240-320q33 0 56.5-23.5T560-640q0-33-23.5-56.5T480-720q-33 0-56.5 23.5T400-640q0 33 23.5 56.5T480-560Zm0-80Zm0 400Z"/>',
  folder: '<path d="M160-160q-33 0-56.5-23.5T80-240v-480q0-33 23.5-56.5T160-800h207q16 0 30.5 6t25.5 17l57 57h320q33 0 56.5 23.5T880-640v400q0 33-23.5 56.5T800-160H160Zm0-80h640v-400H447l-80-80H160v480Zm0 0v-480 480Z"/>',
  shield: '<path d="M480-84q-7 0-13-1t-12-3q-135-45-215-166.5T160-516v-189q0-25 14.5-45t37.5-29l240-90q14-5 28-5t28 5l240 90q23 9 37.5 29t14.5 45v189q0 140-80 261.5T505-88q-6 2-12 3t-13 1Zm0-80q104-33 172-132t68-220v-189l-240-90-240 90v189q0 121 68 220t172 132Zm0-316Z"/>',
  open_in_new: '<path d="M200-120q-33 0-56.5-23.5T120-200v-560q0-33 23.5-56.5T200-840h240q17 0 28.5 11.5T480-800q0 17-11.5 28.5T440-760H200v560h560v-240q0-17 11.5-28.5T800-480q17 0 28.5 11.5T840-440v240q0 33-23.5 56.5T760-120H200Zm560-584L416-360q-11 11-28 11t-28-11q-11-11-11-28t11-28l344-344H600q-17 0-28.5-11.5T560-800q0-17 11.5-28.5T600-840h200q17 0 28.5 11.5T840-800v200q0 17-11.5 28.5T800-560q-17 0-28.5-11.5T760-600v-104Z"/>',
  play_circle: '<path d="M426-330 621-455q14-9 14-25t-14-25L426-630q-15-10-30.5-1.5T380-605v250q0 18 15.5 26.5T426-330Zm54 250q-83 0-156-31.5T197-197q-54-54-85.5-127T80-480q0-83 31.5-156T197-763q54-54 127-85.5T480-880q83 0 156 31.5T763-763q54 54 85.5 127T880-480q0 83-31.5 156T763-197q-54 54-127 85.5T480-80Zm0-80q134 0 227-93t93-227q0-134-93-227t-227-93q-134 0-227 93t-93 227q0 134 93 227t227 93Zm0-320Z"/>',
  arrow_forward: '<path d="M647-440H200q-17 0-28.5-11.5T160-480q0-17 11.5-28.5T200-520h447L451-716q-12-12-11.5-28t12.5-28q12-11 28-11.5t28 11.5l264 264q6 6 8.5 13t2.5 15q0 8-2.5 15t-8.5 13L508-188q-11 11-27.5 11T452-188q-12-12-12-28.5t12-28.5l195-195Z"/>',
  calendar_month: '<path d="M200-80q-33 0-56.5-23.5T120-160v-560q0-33 23.5-56.5T200-800h40v-40q0-17 11.5-28.5T280-880q17 0 28.5 11.5T320-840v40h320v-40q0-17 11.5-28.5T680-880q17 0 28.5 11.5T720-840v40h40q33 0 56.5 23.5T840-720v560q0 33-23.5 56.5T760-80H200Zm0-80h560v-400H200v400Zm0-480h560v-80H200v80Zm0 0v-80 80Zm280 240q-17 0-28.5-11.5T440-440q0-17 11.5-28.5T480-480q17 0 28.5 11.5T520-440q0 17-11.5 28.5T480-400Zm-160 0q-17 0-28.5-11.5T280-440q0-17 11.5-28.5T320-480q17 0 28.5 11.5T360-440q0 17-11.5 28.5T320-400Zm320 0q-17 0-28.5-11.5T600-440q0-17 11.5-28.5T640-480q17 0 28.5 11.5T680-440q0 17-11.5 28.5T640-400ZM480-240q-17 0-28.5-11.5T440-280q0-17 11.5-28.5T480-320q17 0 28.5 11.5T520-280q0 17-11.5 28.5T480-240Zm-160 0q-17 0-28.5-11.5T280-280q0-17 11.5-28.5T320-320q17 0 28.5 11.5T360-280q0 17-11.5 28.5T320-240Zm320 0q-17 0-28.5-11.5T600-280q0-17 11.5-28.5T640-320q17 0 28.5 11.5T680-280q0 17-11.5 28.5T640-240Z"/>',
};

export function icon(name, size = 16) {
  const paths = ICONS[name] || ICONS.source;
  return `<svg class="icon" width="${size}" height="${size}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths}</svg>`;
}

export function materialIcon(name, size = 16) {
  const paths = MATERIAL_ICONS[name] || MATERIAL_ICONS.account_tree;
  return `<svg class="icon material-icon" width="${size}" height="${size}" viewBox="0 -960 960 960" fill="currentColor" stroke="none" aria-hidden="true">${paths}</svg>`;
}

export function hydrateIcons(root = document) {
  root.querySelectorAll("[data-icon]").forEach(element => {
    element.innerHTML = icon(element.dataset.icon, Number(element.dataset.iconSize || 16));
  });
}

export function escapeHtml(value = "") {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

export const markdown = renderMarkdown;

// renderSeqBadge 渲染「序号徽标」：一个大号等宽数字 + 下面一行小字单位。
//
// 出处是 goal 看板的 active seq（goal-board-view.js）：那里用同一形态说"这是本会话
// 第几个目标"。它的价值不在像素，而在**一种视觉只写一份**：字号/间距/大写单位一旦
// 两处各写一份，改配色时必然只改一处（本仓库反复防的漂移）。
//
// tone 只换颜色，不换结构：goal 看板用强调色（--accent），压缩条目用压缩帧那档灰
// （--compaction-frame-latest，与轨迹刻度、对话区分界同一份色）——同一个徽标在两块
// 面板里读起来仍然是同一件东西。
export function renderSeqBadge({ seq, unit, title = "", tone = "", failed = false } = {}) {
  const classes = ["seq-badge"];
  if (tone) classes.push(`is-${tone}`);
  if (failed) classes.push("is-failed");
  return `<span class="${classes.join(" ")}"${title ? ` title="${escapeHtml(title)}"` : ""}>
      <span class="seq-badge-num">${escapeHtml(String(seq ?? ""))}</span>
      <span class="seq-badge-unit">${escapeHtml(String(unit ?? ""))}</span>
    </span>`;
}

export function renderConversationComponent(messages = [], chat = {}, draft = "", anchor = null) {
  const model = renderConversationModel(messages, chat, draft, anchor);
  return { html: model.items.map(item => item.html).join(""), payloads: model.payloads };
}

// DRAFT_ROW_KEY 是页面 context 里「未发送草稿」那一行的稳定 key（渲染层据此对账
// DOM，见 conversation-view.js reconcile）。它不属于任何 message：草稿是投影、
// 不是消息，所以用独立的 chat: 前缀而不是 message:<id>。
export const DRAFT_ROW_KEY = "chat:draft";

// COMPACTION_FRONTIER_KEY 是对话页里「压缩分界」那一行的稳定 key。它不属于任何
// message：分界是会话级事实（单例），所以用独立的 chat: 前缀（同草稿行），渲染层
// 据此对账 DOM。
export const COMPACTION_FRONTIER_KEY = "chat:compaction-frontier";

// insertCompactionFrontier 在分界所在的那条消息之后插入一行「以上已压缩」虚线。
//
// anchor 是 compaction-format.js 里的 conversationCompactionAnchor 返回值（已算好
// 落点与文案；null = 本页不画线）。分界是**会话单例**，因此每个对话页最多一行——
// 历次压缩各插一行，读者会以为两条分界之间的对话还发给模型。
function insertCompactionFrontier(items, anchor) {
  if (!anchor || !anchor.messageID) return items;
  const at = items.findIndex(item => item.messageID && item.messageID === anchor.messageID);
  if (at < 0) return items;
  const rows = items.slice();
  rows.splice(at + 1, 0, { kind: "frontier", key: COMPACTION_FRONTIER_KEY, html: renderCompactionFrontierRow(anchor) });
  return rows;
}

// renderCompactionFrontierRow 渲染「以上已压缩」分界行：左右两条虚线夹一枚说明牌，
// 牌上是三件事——线在哪（label）、上方原文还能不能读（hint，明写不靠悬停）、帧正文
// 怎么打开（按钮）。入口只携带 data 属性（data-compact-frame-ref），由 app.js 的
// document 级委托接住——组件不持有 invoke 依赖（同排队卡片的动作按钮）。
function renderCompactionFrontierRow(anchor) {
  const frameRef = String(anchor.frameRef || "");
  const label = String(anchor.label || "");
  const note = String(anchor.note || "");
  return `<div class="conversation-compaction-frontier" data-conversation-key="${COMPACTION_FRONTIER_KEY}" data-wheel-kind="system" data-wheel-label="${escapeHtml(label)}" title="${escapeHtml(String(anchor.title || label))}">
      <span class="conversation-compaction-frontier-line" aria-hidden="true"></span>
      <span class="conversation-compaction-frontier-chip">
        <strong class="conversation-compaction-frontier-label">${escapeHtml(label)}</strong>
        <em class="conversation-compaction-frontier-hint">分界以上的原文仍在下方，可继续上翻阅读；只是不再随请求发给模型</em>
        ${note ? `<span class="conversation-compaction-frontier-note">${escapeHtml(note)}</span>` : ""}
        ${frameRef ? `<button type="button" class="conversation-compaction-frontier-open" data-compact-frame-ref="${escapeHtml(frameRef)}" title="按 ref ${escapeHtml(frameRef)} 读取压缩帧正文（弹出查看）">查看压缩帧正文</button>` : ""}
      </span>
      <span class="conversation-compaction-frontier-line" aria-hidden="true"></span>
    </div>`;
}

// ── F-6 消息块 / 工具过程块：按邻接决定顶底圆角（融合 vs 分割）─────────────
//
// 用户口径（2026-10-12 拍板）："没有 llm→llm；相同元素如果需要分割，那么就是圆角
// 相对；如果不分割，就是两边都没有圆角"。落成渲染层的判据只有三条：
//   · 不同类（llm 正文块 ↔ 工具过程块）**始终融合** ⇒ 交界两侧圆角都置 0、间距归零；
//   · 同类默认**不分割** ⇒ 同样是"两边都没有圆角"（外侧角保留圆角）；
//   · **分割判据必须显式**：换轮次（round_id）或换角色归属（role_name）＝ 分割
//     ⇒ 两枚块圆角相对（上块下圆角 + 下块上圆角）、中间保留间距。
//   （"没有 llm→llm"= llm→llm 不是一种**独立**情形，走的就是上面这条同类判据。）
//
// 这里写类名同时写 data-*：宿主与测试按数据判，而不是按像素量。
//   is-joined-above / is-joined-below / is-joined-both
//   ⇒ data-joined-above / data-joined-below（"true" / "false"，三枚类只出一枚）
//
// D4 信号条（已按"做不到就写明"的口径登记）：融合只作用于**顶/底圆角与间距**；
// `.message::before` / `.plan-card::before` 仍是按块各自画的信号点，按块保留
// （做成贯通竖条要把点改线，超出本次融合口径，见 styles.css 同段注释）。

// conversationBlockFamily 给出渲染行的"块族"：llm（LLM 正文块）/ tools（工具过程块）
// / other（压缩分界行、草稿行、运行时活动带——它们自成一段，不参与融合）。
function conversationBlockFamily(item) {
  if (item?.kind === "message") return "llm";
  if (item?.kind === "tool" || item?.kind === "tools" || item?.kind === "axis") return "tools";
  return "other";
}

// blockAttribution 取一块的"归属"：llm 块的归属 = 轮次 + 角色归属（round_id /
// role_name，两个字段都已长在元素上）；工具过程块没有这两样（恒为空），因此相邻两
// 段工具过程默认不分割（与同类判据一致）。
function blockAttribution(item) {
  if (item?.kind !== "message") return "";
  return `${Number(item.message?.round_id || 0)}|${String(item.message?.role_name || "")}`;
}

// blocksFuse 判定上下相邻两行是否融合（融合 = 交界处圆角置 0、块间距归零）。
function blocksFuse(previous, next) {
  const previousFamily = conversationBlockFamily(previous);
  const nextFamily = conversationBlockFamily(next);
  if (previousFamily === "other" || nextFamily === "other") return false;
  if (previousFamily !== nextFamily) return true; // 不同类：llm ↔ 工具过程始终融合
  return blockAttribution(previous) === blockAttribution(next); // 同类：默认不分割
}

// joinedAttrs 把"上 / 下是否融合"翻成类名后缀与 data-* 片段（无融合时两者都空，
// 块的既有属性一字不动）。
function joinedAttrs(above = false, below = false) {
  const className = above && below
    ? " is-joined-both"
    : above ? " is-joined-above" : below ? " is-joined-below" : "";
  return {
    className,
    dataAttrs: ` data-joined-above="${above ? "true" : "false"}" data-joined-below="${below ? "true" : "false"}"`
  };
}

// renderConversationModel 把一页的内容组装成渲染行：既定 message + （可选）本页
// 未发送的草稿行 + 运行时活动带。draft 为空/全空白时绝不插行——页面不会凭空多出
// 一条空白草稿（判据同 draft-lifecycle.js composerDraftRows）。
// anchor 是会话单例压缩分界（conversationCompactionAnchor 的返回值；null = 本会话
// 没压缩过、或分界不在本页），只决定「以上已压缩」那一行的落点。
export function renderConversationModel(messages = [], chat = {}, draft = "", anchor = null, streamPhase = "receiving") {
  const payloads = new Map();
  const items = insertCompactionFrontier(buildConversationItems(messages), anchor);
  const grouped = groupConversationItems(items);
  const rendered = grouped.map((item, index) => {
    const key = item.key || `${item.kind}-${index}`;
    // F-6：邻接在渲染期算（只有这里同时看得见上一条与下一条的 kind + 归属）。
    const joinedAbove = index > 0 && blocksFuse(grouped[index - 1], item);
    const joinedBelow = index < grouped.length - 1 && blocksFuse(item, grouped[index + 1]);
    const html = item.kind === "frontier"
      ? item.html
      : item.kind === "tools"
        ? renderToolCall(item.current, item.current.key, joinedAbove, joinedBelow, payloads, item.members.length, item.key, item.members)
      : item.kind === "tool"
        ? renderToolCall(item, key, joinedAbove, joinedBelow, payloads)
        : renderMessage(item.message, key, joinedAbove, joinedBelow, payloads);
    const meta = item.kind === "message"
      ? {
        kind: "message",
        role: item.message?.role || "",
        hasReasoning: Boolean(String(item.message?.reasoning_content || "").trim()),
        hasContent: Boolean(String(item.message?.content || "").trim())
      }
      : item.kind === "tool" || item.kind === "tools"
        ? { kind: "tool", toolCount: item.kind === "tools" ? item.members.length : 1, motion: item.kind === "tools" ? "tool-update" : "" }
        : { kind: item.kind };
    return { key, html, meta };
  });
  // 页面 context = 既定 message + 本页未发送的草稿：草稿排在既有消息之后、
  // 运行时活动带之前——它是"这一页还没提交出去的那一份"（正文由壳层给出，判据
  // 见 draft-lifecycle.js composerDraftRows）。
  const draftText = String(draft ?? "");
  if (draftText.trim() !== "") {
    rendered.push({ key: DRAFT_ROW_KEY, html: renderDraftMessage(draftText, DRAFT_ROW_KEY), meta: { kind: "draft" } });
  }
  const hasRunningTool = grouped.some(item => item.kind === "tools"
    ? item.members.some(member => isToolRunningStatus(member.status))
    : item.kind === "tool" && isToolRunningStatus(item.status));
  const activity = renderChatActivity(chat, streamPhase, { hasRunningTool });
  if (activity) {
    const key = "chat:activity";
    rendered.push({ key, html: `<div class="chat-activity-tail" data-conversation-key="${key}" data-wheel-kind="system" data-wheel-label="${escapeHtml(chat.running ? "执行中" : "等待发送")}">${activity}</div>` });
  }
  return { items: rendered, payloads };
}

// renderChatActivity 渲染对话流尾部的「运行时活动带」——状态文字始终可读，
// 只有调度器确认已进入持续流（100ms 滞回）后才附加局部活动线。
// 排队叠**不在这里**，见 renderMessageQueue：队列是贴着输入框上沿的一叠，宿主是
// #message-queue（输入框正上方），不进对话滚动区。
export function renderChatActivity(chat = {}, streamPhase = "receiving", options = {}) {
  const running = Boolean(chat.running);
  if (!running) return "";
  const hasRunningTool = Boolean(options.hasRunningTool);
  const receiving = streamPhase === "receiving" && !hasRunningTool;
  const label = hasRunningTool ? "Loading tool…" : (receiving ? "Thinking…" : "Loading…");
  return `<section class="runtime-activity" role="status" aria-live="polite" aria-busy="true" data-motion-purpose="feedback" data-motion-trigger="state-change" data-motion-phase="${receiving ? "receiving" : "waiting"}">
    <strong class="runtime-activity-copy loading-shimmer" data-shimmer-text="${label}">${label}</strong>
  </section>`;
}

// renderConversationLoading 渲染会话切换/冷恢复时的局部结构占位。对话区已经
// 知道消息行的大致结构，因此使用 4 条与真实消息同宽的骨架，而不是通用 spinner
// 或整块空白；标题文字为无障碍与低动效环境提供确定的当前状态。
export function renderConversationLoading(mode = "switching") {
  const restoring = mode === "restoring";
  const label = restoring ? "正在恢复会话…" : "正在切换会话…";
  const rows = [
    "is-assistant is-long",
    "is-user is-short",
    "is-assistant is-medium",
    "is-assistant is-short"
  ].map((className, index) => `<div class="conversation-loading-row ${className}" style="--loading-index:${index}"><span></span><span></span></div>`).join("");
  return `<section class="conversation-switch-loading" data-session-loading="${restoring ? "restoring" : "switching"}" role="status" aria-label="${label}" aria-live="polite" aria-busy="true">
    <div class="conversation-loading-stack" aria-hidden="true">${rows}</div>
    <strong class="conversation-loading-caption loading-shimmer" data-shimmer-text="Loading…">Loading…</strong>
  </section>`;
}

// renderMessageQueue 渲染贴输入框上沿的「排队叠」：一行一条（拖拽把手 + 折返箭头
// + 单行正文 + 右侧动作），整叠底边压在输入框上沿上（宿主定位见 .message-queue-host）。
// 它必须跟着输入框走，所以不进对话流：留在滚动区里会被对话内容推走、随滚动跑掉，
// 与输入框之间还隔着一整段对话区底部留白（.conversation 给输入框让位的那一段），
// 读起来跟输入框没有关系（用户口径：间隔太宽，要贴着输入框）。
// 拖动把手与 ↑/↓ 动作都只携带纯数据（data-queue-drag / data-queue-action + 下标），
// 由渲染层（app.js）统一委托到 Bridge，组件本身不持有 invoke 依赖。
export function renderMessageQueue(chat = {}) {
  const queue = Array.isArray(chat.input_queue) ? chat.input_queue : [];
  if (!queue.length) return "";
  return `<section class="message-queue" aria-label="等待发送的消息">
    ${queue.map((input, index) => renderQueuedMessage(input, index, queue.length)).join("")}
  </section>`;
}

// queueDragTarget 把一次拖拽落点折算成后端 ReorderQueuedInput 要的 (from, to)。
// 后端口径是"条目最终落在 to 位置"（application/core/service_queue.go；测试钉住
// [second,third,fourth] --(0,2)--> [third,fourth,second]），而纸面上的落点是**插入缝**：
// 第 hoverIndex 条的上半 = 插到它前面，下半 = 插到它后面。两套口径差一层换算——
// 后端要的是"先把条目抽出去、再插回"的最终下标，抽出这一步会让缝之后的序号前移
// 一位，所以 from 在缝前面时 to 要减 1（否则会把条目往回多送一格）。
// 换算成缝的前一条（to === from，即原地落下）返回 null：不必产生一次真实搬家。
export function queueDragTarget(from, hoverIndex, length, after = false) {
  if (!Number.isInteger(from) || !Number.isInteger(hoverIndex) || !Number.isInteger(length)) return null;
  if (from < 0 || from >= length || hoverIndex < 0 || hoverIndex >= length) return null;
  const seam = after ? hoverIndex + 1 : hoverIndex;
  const to = from < seam ? seam - 1 : seam;
  if (to < 0 || to >= length || to === from) return null;
  return { from, to };
}

// renderQueuedMessage 渲染一条排队输入：**单行条**（用户口径：形状照 Qoder 的队列
// 做法——一行一条：拖拽把手 + 折返箭头 + 单行正文 + 右侧动作），不再是带表头与正文区的卡片。
// 把手（data-queue-drag）承担"拖拽换序"，两侧的 ↑/↓ 动作保留为键盘/无拖拽环境的
// 等价入口——两条路都汇到同一次 ReorderQueuedInput（见 app.js）。
// 正文走纯文本而非 markdown：单行省略号要求这段内容属于该元素本身，块级 <p> 会让
// text-overflow 失效；完整原文挂在 data-tip 上（悬停/聚焦出应用自己的提示，\n 会被
// paintTip 换成 <br>），撤回后原文回到输入框继续编辑。
// 动作按钮只携带纯数据（data-queue-action/index），由渲染层（app.js）统一委托到
// Bridge，组件本身不持有 invoke 依赖。
function renderQueuedMessage(input, index, length) {
  const label = String(index + 1).padStart(2, "0");
  const row = queueRowText(input);
  const move = (action, disabled) => `<button type="button" class="queue-action" data-queue-action="${action}" data-queue-index="${index}"
        title="${action === "up" ? "上移" : "下移"}" aria-label="${action === "up" ? "上移" : "下移"}排队 ${label}"${disabled ? " disabled" : ""}>${icon(action === "up" ? "arrow-up" : "arrow-down", 13)}</button>`;
  return `<article class="queued-message" data-queue-index="${index}" aria-label="排队 ${label}，等待发送">
      <span class="queued-drag-handle" draggable="true" data-queue-drag="${index}" title="拖拽调整排队顺序" aria-label="拖拽调整排队 ${label} 的顺序">${icon("grip", 13)}</span>
      <span class="queued-message-lead" aria-hidden="true">${icon("corner-down-right", 14)}</span>
      <span class="queued-message-text"${row.tip ? ` data-tip="${escapeHtml(row.tip)}"` : ""}>${escapeHtml(row.line)}</span>
      <span class="queued-message-actions">
        ${move("up", index === 0)}
        ${move("down", index === length - 1)}
        <button type="button" class="queue-action" data-queue-action="recall" data-queue-index="${index}"
          title="撤回编辑" aria-label="撤回排队 ${label} 到输入框">${icon("recall", 13)}</button>
      </span>
    </article>`;
}

// queueRowText 把排队正文压成单行条要的两份文本：line 是条上显示的那一行（连续
// 空白压缩成单个空格，免得换行把一行撕出空洞），tip 是完整原文（保留换行，交给
// data-tip）。两份都原样交给 escapeHtml，正文里的标签不会被当成 HTML。
function queueRowText(value) {
  const original = String(value ?? "");
  return { line: original.replace(/\s+/g, " ").trim(), tip: original.trim() };
}

// renderDraftMessage 渲染「未发送草稿」行：本会话当前还没提交出去的正文（仍活在
// 输入框里，生命周期见 draft-lifecycle.js）。形状与排队条同族（单行条），差别用
// 强调色 + 右侧「待发送」标签表达。身份有三处可判：meta.kind="draft"、`is-draft`
// 类、`data-draft`/`data-unsent` 标记——渲染层与测试都不靠正文猜。
// 它是投影：不进 conversation、不派 message id、也不回写输入框（输入框正文才是
// 本地事实源）。
function renderDraftMessage(text, key) {
  const row = queueRowText(text);
  return `<article class="queued-message draft-message is-draft" data-conversation-key="${escapeHtml(key)}" data-draft="true" data-unsent="true" aria-label="未发送草稿，待发送">
      <span class="queued-message-lead" aria-hidden="true">${icon("message", 14)}</span>
      <span class="queued-message-text"${row.tip ? ` data-tip="${escapeHtml(row.tip)}"` : ""}>${escapeHtml(row.line)}</span>
      <span class="queued-message-flag">待发送</span>
    </article>`;
}

// queueMoveTarget 把队列条目的 ↑/↓ 动作映射为一次调换（from → to）。边界
// 动作（首行上移 / 末行下移）与非法下标返回 null——与渲染层按钮 disabled
// 的判定同源，渲染层与测试共用这一份规则。
export function queueMoveTarget(action, index, length) {
  if (action !== "up" && action !== "down") return null;
  if (!Number.isInteger(index) || index < 0 || index >= length) return null;
  const to = action === "up" ? index - 1 : index + 1;
  if (to < 0 || to >= length) return null;
  return { from: index, to };
}

export function buildConversationItems(messages = []) {
  const items = [];
  // 配对键 = 框架 tool-call id（tool.started 与 tool_result 同源）；不按名字回退
  // 猜配对，否则同名并发工具会被并成一行。
  const pendingByID = new Map();
  for (const [messageIndex, message] of messages.entries()) {
    if (!message.tool) {
      // messageID：分界行要按事件序号与消息行对位（判据见 compaction-format.js 的
      // messageOrdinal / conversationCompactionAnchor），
      // 因此每条渲染行都带上自己的消息 id，不靠渲染层回查 messages。
      items.push({ kind: "message", key: `message:${message.id || messageIndex}`, messageID: String(message.id || ""), message });
      continue;
    }
    const tool = message.tool;
    const isOutput = message.role === "tool_result";
    if (isOutput) {
      const target = tool.id ? pendingByID.get(tool.id) : null;
      if (target) {
        target.output = tool.error || tool.result || message.content || "";
        target.error = tool.error || "";
        target.status = tool.error ? "error" : (tool.status || "success");
        target.duration = tool.duration || target.duration;
        target.outputAttached = true;
        target.resultRef = tool.result_ref || "";
        target.truncated = Boolean(tool.truncated);
        target.totalChars = Number(tool.total_chars) || 0;
        continue;
      }
    }
    const item = {
      kind: "tool",
      key: `tool:${tool.id || message.id || messageIndex}`,
      messageID: String(message.id || ""),
      id: tool.id || "",
      name: tool.name || "tool",
      input: tool.arguments || "",
      output: tool.error || tool.result || (isOutput ? message.content || "" : ""),
      error: tool.error || "",
      status: tool.status || (isOutput ? "success" : "pending"),
      duration: tool.duration || 0,
      outputAttached: isOutput || Boolean(tool.result || tool.error),
      resultRef: tool.result_ref || "",
      truncated: Boolean(tool.truncated),
      totalChars: Number(tool.total_chars) || 0
    };
    items.push(item);
    if (item.id) pendingByID.set(item.id, item);
  }
  return items;
}

function hasVisibleAssistantOutput(message) {
  if (!message || message.role !== "assistant") return false;
  return String(message.content || "").trim().length > 0;
}

// 连续工具调用组成一个“工具域”：对话区只露出该域最新的那一条，历史工具仍在
// 轨迹页完整可查。消息、压缩分界与空 assistant 占位都会切断工具域。
function groupConversationItems(items) {
  const grouped = [];
  let pendingTools = [];
  const flushTools = () => {
    if (!pendingTools.length) return;
    if (pendingTools.length === 1) {
      grouped.push(pendingTools[0]);
    } else {
      const latest = pendingTools.at(-1);
      grouped.push({
        kind: "tools",
        key: `tools:${pendingTools[0].key}`,
        members: pendingTools.slice(),
        current: latest
      });
    }
    pendingTools = [];
  };
  for (const item of items) {
    if (item.kind === "frontier") {
      flushTools();
      grouped.push(item);
      continue;
    }
    if (item.kind === "tool") {
      pendingTools.push(item);
      continue;
    }
    if (item.kind !== "message") {
      flushTools();
      grouped.push(item);
      continue;
    }
    const message = item.message;
    if (message.role === "user") {
      flushTools();
      grouped.push(item);
      continue;
    }
    if (message.role === "assistant" && !hasVisibleAssistantOutput(message)) {
      // 结构性空 assistant 占位：正文到达前不显示，到达后按 LLM 输出渲染。
      continue;
    }
    flushTools();
    grouped.push(item);
  }
  flushTools();
  return grouped;
}

export function renderSources(sources = []) {
  if (!sources.length) return '<span class="muted list-empty">暂无 Agent 已读文件</span>';
  return sources.map(source => {
    const sourceIcon = source.kind === "capability" ? "folder" : "file";
    return `<article class="source-item">
      <span class="source-icon">${icon(sourceIcon, 15)}</span>
      <div><strong>${escapeHtml(source.name || source.path)}</strong><small title="${escapeHtml(source.path || "")}">${escapeHtml(source.path || "")}</small></div>
      <span class="source-kind">${escapeHtml(shortKind(source.kind))}</span>
    </article>`;
  }).join("");
}

// wheelSnippet 把消息正文压成轮轴标签用的一行摘要。
export function wheelSnippet(text = "", limit = 32) {
  const flat = String(text).replace(/\s+/g, " ").trim();
  if (!flat) return "（空）";
  return flat.length > limit ? `${flat.slice(0, limit)}…` : flat;
}

// renderMessageBody 渲染消息正文：**用户消息**把「用户附的文件引用」（`@路径` / 裸路径）
// 渲染成 chip（file-reference.js 的同一枚 chip，正文宿主），其余角色保持原样。
// chip 只是渲染层的识别：正文文本一个字都不改，roll/wheel/agent 看到的仍是完整路径
// （用户附的文件因此能在正文里被看见，agent 据此自己 read_file/grep）。
//
// `key` 是这一行的对话 key：随正文一起传给 markdown，让沙箱渲染块拿到稳定身份
// `<行 key>#<围栏序号>`（见 markdown.js 的 nextEmbedKey 与 embed-buffering.js）。
function renderMessageBody(message, role, key = "") {
  const content = message.content || "";
  const options = { rowKey: String(key || "") };
  return role === "user" ? markdown(content, { ...options, fileRefs: true }) : markdown(content, options);
}

function renderMessage(message, key, above = false, below = false, payloads = new Map()) {
  const role = message.role || "assistant";
  const joined = joinedAttrs(above, below);
  const label = roleIdentity(message, role);
  const reasoning = String(message.reasoning_content || "").trim();
  const roundID = Number(message.round_id || 0);
  const images = role === "user" ? messageImages(message) : [];
  const wheelKind = role === "user" ? "user" : role === "system" ? "system" : role === "assistant" ? (String(message.content || "").trim() ? "agent" : "think") : "other";
  // 轮轴标签：正文为空但带了图时不能写成「（空）」——用户确实发了东西，发的是图。
  const wheelText = String(message.content || "").trim() || reasoning || (images.length ? `图片 ×${images.length}` : "");
  const wheelLabel = `${role === "user" ? "你" : role === "system" ? "系统" : label} · ${wheelSnippet(wheelText)}`;
  const actions = renderUserMessageActions(message, key, payloads);
  const body = `<div class="message-body" data-message-body="true">${renderMessageBody(message, role, key)}</div><!--/seelex-message-body-->`;
  // 图片挂在正文**下面**：用户发出去的是「这句话 + 这几张图」，读的顺序也应当
  // 是"先说什么，再看给的是什么"。缩略图字节由 message-images.js 注水（一次
  // RPC 一张，按 ref 缓存），这里只画结构与 ref。
  const imageStrip = role === "user" ? renderMessageImages(images) : "";
  const content = role === "user"
    ? `<div class="message-user-cluster">${body}${imageStrip}${actions}</div>`
    : `${body}${actions}`;
  return `<article class="message ${escapeHtml(role)}${messageRoleClass(message)}${joined.className}" data-conversation-key="${escapeHtml(key)}" data-trajectory-key="${escapeHtml(key)}" data-wheel-kind="${wheelKind}" data-wheel-label="${escapeHtml(wheelLabel)}" data-role-name="${escapeHtml(String(message.role_name || ""))}" data-round-id="${roundID}"${joined.dataAttrs}>
    ${content}
  </article>`;
}

// messageImages 取一条消息随行的图片（只有用户行会有；其它角色一律空）。
//
// 判据只有一处：`message.images`（后端把用户消息随行图片写在这里，见
// protocol.Message.Images）。前端不从正文、不从队列、不从输入框状态猜"这条消息
// 是不是带了图"——猜出来的东西在刷新后一定对不上。
function messageImages(message) {
  const list = Array.isArray(message?.images) ? message.images : [];
  return list.filter(image => String(image?.ref ?? "") !== "");
}

// renderMessageImages 画用户消息的图片条；空列表整块收起（不吃高度、不吃指针）。
function renderMessageImages(images) {
  if (!images.length) return "";
  const figures = images.map(image => {
    const ref = String(image.ref);
    const name = String(image?.name ?? "") || "图片";
    const dimensions = Number(image?.width) > 0 && Number(image?.height) > 0
      ? `${Number(image.width)}×${Number(image.height)}`
      : "";
    const title = dimensions === "" ? name : `${name}（${dimensions}）`;
    return `<figure class="message-image" data-message-image-figure="${escapeHtml(ref)}">`
      + `<button type="button" class="message-image-open" data-message-image-open="${escapeHtml(ref)}"`
      + ` title="点击用系统默认查看器打开 ${escapeHtml(title)}" aria-label="打开图片 ${escapeHtml(title)}">`
      + `<img class="message-image-thumb" data-message-image="${escapeHtml(ref)}" alt="${escapeHtml(name)}" loading="lazy" draggable="false" />`
      + `</button>`
      + `</figure>`;
  }).join("");
  return `<div class="message-images" role="group" aria-label="随这条消息发送的图片">${figures}</div>`;
}

// renderUserMessageActions 只给真实用户行生成复制/编辑动作。复制正文进入既有
// payload Map（由 conversation-view 的 copyText 通道消费）；编辑是否可用只认
// 后端字段 editable/event_seq，前端不按窗口条数推断。
function renderUserMessageActions(message, key, payloads) {
  if (message?.role !== "user") return "";
  const content = String(message.content ?? "");
  if (!content.trim()) return "";
  const copyKey = `message-copy:${key}`;
  payloads.set(copyKey, content);
  const eventSeq = Number(message.event_seq || 0);
  const editable = message.editable === true && Number.isSafeInteger(eventSeq) && eventSeq > 0;
  const reason = String(message.edit_disabled_reason || "").trim();
  const editTitle = editable ? "编辑并分叉发送" : (reason || "这条消息不能编辑");
  return `<div class="message-actions" role="group" aria-label="用户消息操作">
    <button type="button" class="message-action" data-copy="${escapeHtml(copyKey)}" title="复制原文" aria-label="复制这条用户消息">${icon("copy", 14)}<span>复制</span></button>
    <button type="button" class="message-action" data-edit-message="${escapeHtml(key)}" data-event-seq="${eventSeq}" data-edit-disabled="${editable ? "false" : "true"}" title="${escapeHtml(editTitle)}" aria-label="${editable ? "编辑这条用户消息并分叉" : escapeHtml(`不能编辑：${editTitle}`)}" aria-disabled="${editable ? "false" : "true"}">${icon("edit", 14)}<span>${editable ? "编辑" : "不可编辑"}</span></button>
  </div>`;
}

// messageRoleClass 给群聊归属加一层视觉分区（CSS class 后缀）：同一轮对话里
// EXEC（main）与 ADVISOR（tl）都是 provider role=assistant，光看 role 分不出来；
// 背景色于是成为"谁在说话"的第一线索。无归属的消息（普通 assistant）不加类，
// 保持既有观感不变。
export function messageRoleClass(message) {
  const roleName = String(message?.role_name || "").trim();
  if (roleName === "main") return " is-exec";
  if (roleName === "tl" || roleName === "techlead") return " is-advisor";
  if (roleName === "user") return " is-user";
  if (roleName) return " is-role";
  return "";
}

// roleIdentity 返回消息所属 agent 的展示身份：群聊角色归属优先
// （main → EXEC、tl/techlead → ADVISOR），无归属时回退到 provider role 文案。
// 两个 agent 都渲染成同一个 AGENT/Seelex 会让"主持该轮次的 agent"不可辨。
export function roleIdentity(message, role = message?.role || "assistant") {
  const roleName = String(message?.role_name || "").trim();
  if (roleName && roleName !== "user") {
    if (roleName === "main") return "EXEC";
    if (roleName === "tl" || roleName === "techlead") return "ADVISOR";
    return roleName.toUpperCase();
  }
  return role === "user" ? "YOU" : role === "assistant" ? "AGENT" : role.toUpperCase();
}

// renderToolDomainList 渲染工具域的抽屉列表：全部条目按执行顺序排在**固定高度的
// 可滚动窗口**里（`[data-scroll-window]`，高度与滚动条样式见 styles.css）。
//
// 为什么是滚动窗口而不是折叠/分页：一段连续的工具过程能长到几十次（后台测试轮询、
// 大目录扫描、修复循环），条目本身一条都不该藏起来——用户要的是"整段在那，我上下
// 滑着看"，而不是"先点开一批、再点开另一批"。窗口高度有界，对话流就不会被一堵墙
// 顶走；滚动位置在增量重绘时由 conversation-view 的 captureUIState 保回来。
function renderToolDomainList(members, payloads) {
  return `<div class="tool-domain-list" data-scroll-window="tool-domain" tabindex="0">${members.map(member => renderToolCall(member, member.key, false, false, payloads)).join("")}</div>`;
}

// renderToolCall 渲染一条工具调用：summary 是与左栏会话行同族的单行摘要，
// details 展开后才是入参/出参抽屉。摘要与抽屉共用一个 key，展开态由浏览器原生
// <details> 保存，增量重绘时由 conversation-view 的 UI state 原样恢复。
function renderToolCall(tool, key, above = false, below = false, payloads = new Map(), toolCount = 1, nodeKey = key, domainMembers = null) {
  const status = statusMeta(tool.status, tool.error);
  const joined = joinedAttrs(above, below);
  const running = status.phase === "running";
  const invocation = toolInvocationSummary(tool);
  const inputKey = `${key}-in`;
  const outputKey = `${key}-out`;
  const groupLabel = toolCount > 1 ? ` · 最近 1/${toolCount}` : "";
  const wheelLabel = `${tool.name} · ${status.label}${invocation ? ` · ${invocation}` : ""}${groupLabel}`;
  const metrics = [formatDuration(tool.duration), chatChipSize(tool)].filter(Boolean).join(" · ");
  const isDomain = Array.isArray(domainMembers) && domainMembers.length > 1;
  const drawer = isDomain
    ? renderToolDomainList(domainMembers, payloads)
    : renderToolIODetail({
      input: tool.input,
      output: tool.output,
      inputKey,
      outputKey,
      record: tool,
      statusClass: status.className,
      payloads
    });
  return `<details class="tool-run ${status.className} tool-call-row${isDomain ? " tool-domain" : ""}${joined.className}" data-conversation-key="${escapeHtml(nodeKey)}" data-trajectory-key="${escapeHtml(key)}" data-tool-id="${escapeHtml(tool.id || key)}" data-tool-count="${toolCount}"${isDomain ? ' data-tool-domain="true"' : ""} data-wheel-kind="tools" data-wheel-label="${escapeHtml(wheelLabel)}" aria-busy="${running ? "true" : "false"}"${joined.dataAttrs}>
    <summary class="tool-call-summary" title="${running ? `正在运行 ${escapeHtml(tool.name)}` : `${escapeHtml(tool.name)} ${escapeHtml(status.label)}`}${toolCount > 1 ? `；最近 1/${toolCount} 次工具调用` : ""}；展开查看入参和出参">
      <span class="tool-call-caret" aria-hidden="true"></span>
      <span class="tool-symbol">${icon("terminal", 13)}</span>
      <span class="tool-name-cluster"><strong class="chat-chip-name tool-call-name" data-tool-label="${escapeHtml(tool.name)}" data-motion-purpose="feedback" data-motion-trigger="state-change" data-motion-phase="${running ? "running" : status.phase}">${escapeHtml(tool.name)}</strong>${toolCount > 1 ? `<span class="tool-group-count" title="这一工具域共 ${toolCount} 次调用；此处只显示最新一次">${toolCount}</span>` : ""}</span>
      ${invocation ? `<span class="tool-command">${escapeHtml(invocation)}</span>` : '<span class="tool-command"></span>'}
      <span class="tool-state">${icon(status.icon, 11)} ${status.label}</span>
      <span class="tool-meta">${escapeHtml(metrics)}</span>
    </summary>
    <div class="tool-call-drawer${isDomain ? " tool-domain-drawer" : ""}">
      <header class="tool-call-drawer-head">
        <span>${isDomain ? `本段 ${toolCount} 次工具调用` : "工具调用"}</span>
        <code>${escapeHtml(isDomain ? "先选条目，再展开入参/出参" : (tool.id || key))}</code>
        <button type="button" class="tool-call-trajectory" data-tool-trajectory="${escapeHtml(key)}" title="在轨迹页查看并定位这条工具调用">${icon("source", 12)}<span>在轨迹中查看</span></button>
      </header>
      ${drawer}
    </div>
  </details>`;
}

// renderToolIODetail 是对话工具抽屉与轨迹 inspector 共用的唯一 IN/OUT 渲染件：
// 可解析 JSON 时格式化，超长输出走 result_ref 分页读回；复制与展开只改展示，
// 完整 payload 写入调用方提供的 Map。
export function renderToolIODetail({
  input,
  output,
  inputKey,
  outputKey,
  record = {},
  statusClass = "",
  payloads = new Map(),
  mode = "inline",
  defaultOpen = false
} = {}) {
  const inputText = toolPrettyValue(input);
  const outputText = toolPrettyValue(output);
  payloads.set(inputKey, inputText);
  payloads.set(outputKey, outputText);
  const think = String(record.reasoning || "").trim();
  if (think) payloads.set(`${outputKey}-think`, think);

  const inputView = toolLimitText(inputText, 1400, 28);
  const outputView = toolLimitText(outputText, 4000, 40);
  const ref = record.resultRef || "";
  const note = record.truncated && ref
    ? `<span class="io-note">预览 ${outputView.total} 字符 · 全文 ${record.totalChars} 字符（默认折叠，点击加载）</span>`
    : outputView.truncated
      ? `<span class="io-note">预览 ${outputView.total} 字符</span>`
      : "";
  const fullButton = ref && record.truncated
    ? `<button class="io-expand" type="button" data-load-ref="${escapeHtml(ref)}" title="加载完整输出（按需拉取，默认折叠）">${icon("expand", 12)} <span>加载完整输出</span></button>`
    : outputView.truncated
      ? `<button class="io-expand" type="button" data-expand="${escapeHtml(outputKey)}" title="展开完整内容">${icon("expand", 12)} <span>+${outputView.hidden} chars</span></button>`
      : "";
  // 抽屉打开即展示有界预览；超长全文由下面的 data-expand / data-load-ref
  // 再拉取，不再多嵌一层“查看输出”折叠，避免点开工具行后仍只看到空壳。
  const outBody = `<pre class="trajectory-json-value">${escapeHtml(outputView.preview || "（无出参）")}</pre>`;
  const drawer = mode === "drawer";
  const drawerOpen = drawer && defaultOpen === true;
  const closeButton = (key, title) => drawer
    ? `<button class="icon-button subtle io-drawer-close" type="button" data-io-drawer-close="${escapeHtml(key)}" title="关闭${escapeHtml(title)}" aria-label="关闭${escapeHtml(title)}">${icon("close", 13)}</button>`
    : "";
  const panelHeader = (label, key, total, title, open = drawerOpen) => drawer
    ? `<header>
        <button class="io-drawer-toggle" type="button" data-io-drawer="${escapeHtml(key)}" aria-expanded="${open}" title="${open ? "收起" : "展开"}${escapeHtml(title)}">
          <span class="io-drawer-chevron" aria-hidden="true"></span>
          <span class="io-label">${escapeHtml(label)}</span>
          <span class="io-meta">${total} chars</span>
        </button>
        <button class="icon-button subtle" type="button" data-copy="${escapeHtml(key)}" title="复制${escapeHtml(title)}" aria-label="复制${escapeHtml(title)}">${icon("copy", 13)}</button>
        ${closeButton(key, title)}
      </header>`
    : `<header><span class="io-label">${escapeHtml(label)}</span><span class="io-meta">${total} chars</span><button class="icon-button subtle" type="button" data-copy="${escapeHtml(key)}" title="复制${escapeHtml(title)}" aria-label="复制${escapeHtml(title)}">${icon("copy", 13)}</button></header>`;
  const panelBody = (body, key, open = drawerOpen) => drawer
    ? `<div class="io-drawer-content" data-io-drawer-content="${escapeHtml(key)}"${open ? "" : " hidden"}>${body}</div>`
    : body;
  const thinkPanel = think
    ? `<section class="io-panel trajectory-think" data-payload="${escapeHtml(`${outputKey}-think`)}">
        ${panelHeader("THINK", `${outputKey}-think`, toolLimitText(think, 4000, 40).total, "思考内容", false)}
        ${panelBody(`<pre>${escapeHtml(toolLimitText(think, 4000, 40).preview)}</pre>`, `${outputKey}-think`, false)}
      </section>`
    : "";

  return `${thinkPanel}<div class="trajectory-io-grid">
    <section class="io-panel${drawerOpen ? " is-drawer-open" : ""}" data-payload="${escapeHtml(inputKey)}" data-json-kind="input" aria-label="入参 JSON">
      ${panelHeader("入参 JSON", inputKey, inputView.total, "入参")}
      ${panelBody(`<pre class="trajectory-json-value">${escapeHtml(inputView.preview || "（无入参）")}</pre>`, inputKey)}
    </section>
    <section class="io-panel ${statusClass === "is-error" ? "io-error" : ""}${drawerOpen ? " is-drawer-open" : ""}" data-payload="${escapeHtml(outputKey)}" data-json-kind="output" aria-label="出参 JSON">
      ${panelHeader("出参 JSON", outputKey, outputView.total, "出参")}
      ${panelBody(`${outBody}${note}${fullButton}`, outputKey)}
    </section>
  </div>`;
}

function toolPrettyValue(value) {
  const text = String(value || "").trim();
  if (!text) return "";
  try {
    return JSON.stringify(JSON.parse(text), null, 2);
  } catch {
    return text;
  }
}

function toolLimitText(value, maxChars, maxLines) {
  const text = String(value || "");
  const lines = text.split("\n");
  let preview = lines.slice(0, maxLines).join("\n");
  if (preview.length > maxChars) preview = preview.slice(0, maxChars);
  const truncated = preview.length < text.length;
  return { preview, truncated, hidden: Math.max(text.length - preview.length, 0), total: text.length };
}

function toolInvocationSummary(tool) {
  const raw = String(tool.input || "").trim();
  if (!raw) return "";
  let summary = raw;
  try {
    const parsed = JSON.parse(raw);
    summary = String(
      parsed?.command || parsed?.cmd || parsed?.path || parsed?.file_path ||
      parsed?.pattern || parsed?.query || parsed?.url || ""
    ).trim();
    if (!summary && parsed && typeof parsed === "object") summary = JSON.stringify(parsed);
  } catch {
    // Non-JSON tool arguments remain visible as their original compact text.
  }
  summary = summary.replace(/\s+/g, " ").trim();
  return summary.length > 180 ? `${summary.slice(0, 177)}…` : summary;
}

function chatChipSize(tool) {
  const chars = Number(tool.totalChars) || String(tool.output || "").length;
  if (chars <= 0) return "";
  return chars < 1024 ? `${chars} B` : `${(chars / 1024).toFixed(chars < 10240 ? 1 : 0)} KB`;
}

function statusMeta(status, error) {
  if (error || status === "error" || status === "failed") return { phase: "error", label: "失败", icon: "error", className: "is-error" };
  if (status === "cancelled" || status === "canceled" || status === "aborted" || status === "interrupted") {
    return { phase: "cancelled", label: "已取消", icon: "stop", className: "is-cancelled" };
  }
  if (isToolRunningStatus(status)) return { phase: "running", label: "正在运行", icon: "source", className: "is-running" };
  return { phase: "success", label: "已完成", icon: "check", className: "is-success" };
}

function isToolRunningStatus(status) {
  return status === "running" || status === "pending";
}

function formatDuration(duration) {
  const milliseconds = Number(duration) / 1e6;
  if (!Number.isFinite(milliseconds) || milliseconds <= 0) return "";
  return milliseconds >= 1000 ? `${(milliseconds / 1000).toFixed(1)}s` : `${Math.round(milliseconds)}ms`;
}

function shortKind(kind = "source") {
  return ({ documentation: "DOC", configuration: "CFG", capability: "CAP", read: "READ" })[kind] || String(kind).slice(0, 3).toUpperCase();
}
