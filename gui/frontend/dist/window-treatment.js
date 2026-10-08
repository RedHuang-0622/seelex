// 窗口后处理责任链：**皮肤 → 窗口材质**。
//
// 背景：毛玻璃皮肤（themes/glass.css）要真"透出桌面"，窗口本身得让出底色——而
// Wails 的窗口透明/亚克力只能在**建窗时**设，没有运行时开关。所以这里只做一件事：
// 把"当前皮肤要哪种窗口"从皮肤清单里确认出来，落成一个词，交给 Go 侧在下次启动时读
// （Bridge.SetWindowAppearance → run_wails 启动期 readWindowAppearance）。
//
// 责任链：链上每一节认领一组皮肤、给出它的窗口材质；**第一个认领的胜出**；没有一节
// 认领 → 默认 opaque（现有一切皮肤都不受影响）。要加新的"材质皮肤"，只往
// WINDOW_HANDLERS 里插一节即可：Go 侧、组件层、其它皮肤都不用动。
//
// 为什么用词（opaque/acrylic）而不是布尔：窗口材质未来可能不止"透/不透"一种
// （Mica / 纯透明 …），词比布尔宽，且与 Wails `Windows.BackdropType` 同构。
const WINDOW_HANDLERS = [
  // 毛玻璃：窗口走亚克力，CSS 里的半透明面与 backdrop-filter 才有东西可透。
  { id: "glass-material", skins: ["glass"], treatment: "acrylic" }
];

// WINDOW_TREATMENT_OPAQUE 是链路尽头的默认（也是任何"不认识这个词"的回落）。
export const WINDOW_TREATMENT_OPAQUE = "opaque";

// resolveWindowTreatment 走一遍责任链，返回当前皮肤该用的窗口材质。
// handlers 可注入，测试才好摆出一条自定义链验"第一个认领的胜出"。
export function resolveWindowTreatment(skinId, handlers = WINDOW_HANDLERS) {
  const id = String(skinId ?? "").trim().toLowerCase();
  if (id === "") return WINDOW_TREATMENT_OPAQUE;
  for (const handler of handlers) {
    const skins = handler?.skins;
    if (Array.isArray(skins) && skins.includes(id)) {
      return handler.treatment || WINDOW_TREATMENT_OPAQUE;
    }
  }
  return WINDOW_TREATMENT_OPAQUE;
}
