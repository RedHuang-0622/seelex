// file-drop.js — 从档案夹（右栏工作树）把文件"抽出来"发到会话/消息队列的纯逻辑。
//
// 交互链：工作树文件行 draggable（worktree-view.js 输出 data-file-drag）→
// dragstart 把**相对路径**写进 dataTransfer（自定义 mime + text/plain 兜底）→
// 聊天框/消息队列是 drop 目标 → 落点决定"追加进草稿"还是"直接排队发出去"。
//
// 这一层只做纯映射（拖拽载荷 → 路径 → 排队正文 → 动作），DOM 与 Bridge 调用留在
// app.js：WebView 里拖放事件的形状（自定义 mime / text/plain / files）在单测里
// 复现不出来，能复现、也最该钉住的是"载荷怎么读、路径怎么变成一条排队消息"。

export const FILE_DRAG_MIME = "application/x-seelex-file";

// MAX_DROP_PATH 是单条拖拽路径的长度上限：超长的多半不是路径（整段正文被当成
// 路径拖过来），直接丢弃比发一条读不出来的指令好。
const MAX_DROP_PATH = 512;

// normalizeDropPath 规整一条拖拽路径：统一分隔符、去掉 ./ 前缀、拒绝绝对路径、
// 任何 .. 段与空串（"工作树文件"必须是工作区内的相对路径——绝对路径发进会话只会
// 让模型去读一个与本工作区无关的文件）。取不出合法相对路径返回 ""，调用方据此整条
// 丢弃：不猜、不回退到文件名。
export function normalizeDropPath(raw) {
  const value = typeof raw === "string" ? raw.trim() : "";
  if (value === "" || value.length > MAX_DROP_PATH) return "";
  const path = value.replace(/\\/g, "/").replace(/^\.\/+/, "");
  if (path === "" || path.startsWith("/") || /^[a-zA-Z]:/.test(path)) return "";
  if (path.split("/").some(segment => segment === "..")) return "";
  return path;
}

// fileDragPayload 是 dragstart 要写进 dataTransfer 的载荷（路径非法 → null，
// 该行就不该启动拖拽）。
export function fileDragPayload(raw) {
  const path = normalizeDropPath(raw);
  return path === "" ? null : { mime: FILE_DRAG_MIME, path };
}

// dropPaths 从一次 drop 的载荷里取出全部可用的工作树路径（去重、保序）：
//   1) 自定义 mime（本应用自己的拖拽，多选时一行一条）；
//   2) text/plain（跨窗口/浏览器兜底）；
//   3) dataTransfer.files 里带**路径字段**的条目（系统文件拖入只有文件名，
//      不是工作树相对路径，因此不接受裸 name —— 发出去只会读到不存在的文件）。
export function dropPaths({ mime = "", text = "", files = [] } = {}) {
  const candidates = [];
  const pushLines = value => {
    if (typeof value === "string" && value !== "") candidates.push(...value.split("\n"));
  };
  pushLines(mime);
  pushLines(text);
  for (const file of Array.isArray(files) ? files : []) {
    if (typeof file === "string") {
      candidates.push(file);
      continue;
    }
    if (file && typeof file.path === "string") candidates.push(file.path);
  }
  const paths = [];
  for (const candidate of candidates) {
    const path = normalizeDropPath(candidate);
    if (path !== "" && !paths.includes(path)) paths.push(path);
  }
  return paths;
}

// fileQueueText 把一批路径渲染成一条排队消息的正文：给一句可执行的指令（读这些
// 文件）+ 逐条路径，模型据此直接发起 read_file，不需要猜"用户拖进来是什么意思"。
export function fileQueueText(paths) {
  const list = (Array.isArray(paths) ? paths : []).filter(path => typeof path === "string" && path !== "");
  if (list.length === 0) return "";
  const subject = list.length === 1 ? "工作树文件" : `以下 ${list.length} 个工作树文件`;
  return `读取${subject}：\n${list.map(path => `- ${path}`).join("\n")}`;
}

// appendDropText 把文件引用接在草稿之后：草稿是用户正在写的那句话，拖进来的文件
// 只能**追加**，不能替换（也不替他按下发送）。
export function appendDropText(draftText, text) {
  const draft = typeof draftText === "string" ? draftText : "";
  if (typeof text !== "string" || text === "") return draft;
  if (draft.trim() === "") return text;
  return `${draft.replace(/\s+$/, "")}\n\n${text}`;
}

// dropPlan 决定这次落地怎么走（纯函数，返回 null = 没拖到可用路径，什么都不做）：
//
//   - 草稿非空 → "insert"：把文件引用追加到草稿末尾。用户正在写的话不能被打断，
//     更不能替他发出去；
//   - 草稿为空 → "queue"：整条交出去发送。会话正在跑时后端把它排进**消息队列**
//     （渲染成聊天框上沿的排队卡片），空闲时就是一条普通消息。
export function dropPlan({ paths = [], draftText = "" } = {}) {
  const list = [];
  for (const candidate of Array.isArray(paths) ? paths : []) {
    const path = normalizeDropPath(candidate);
    if (path !== "" && !list.includes(path)) list.push(path);
  }
  if (list.length === 0) return null;
  const text = fileQueueText(list);
  const draft = typeof draftText === "string" ? draftText : "";
  if (draft.trim() === "") return { mode: "queue", paths: list, text };
  return { mode: "insert", paths: list, text, draft: appendDropText(draft, text) };
}
