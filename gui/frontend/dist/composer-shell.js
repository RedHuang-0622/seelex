// composer-shell.js 是输入框 DOM 的唯一装配件。
//
// index.html 只保留一份 <template id="composer-template">：主会话在模块初始化时
// 把模板克隆进 #composer，子代理会话再按独立前缀克隆到自己的弹层。这样主/子会话
// 共用同一套 textarea、状态行、发送键和样式；前缀只解决 DOM id 不能重复的问题，
// 不复制业务状态，也不在这里调用 Bridge。

const ID_REFERENCE_ATTRIBUTES = ["for", "aria-controls", "aria-labelledby", "aria-describedby", "list", "form"];

function prefixedID(id, prefix) {
  return prefix ? `${prefix}-${id}` : id;
}

function rekeyComposer(root, prefix) {
  if (!prefix) return;
  const idMap = new Map();
  for (const element of root.querySelectorAll("[id]")) {
    const oldID = element.id;
    const nextID = prefixedID(oldID, prefix);
    idMap.set(oldID, nextID);
    element.id = nextID;
  }
  for (const element of root.querySelectorAll(`[${ID_REFERENCE_ATTRIBUTES.join("],[")}]`)) {
    for (const attribute of ID_REFERENCE_ATTRIBUTES) {
      const value = element.getAttribute(attribute);
      if (!value) continue;
      element.setAttribute(attribute, value.split(/\s+/).map(token => idMap.get(token) || token).join(" "));
    }
  }
  for (const label of root.querySelectorAll("label[for]")) {
    const target = label.getAttribute("for");
    if (target) label.setAttribute("for", idMap.get(target) || target);
  }
}

export function composerControlIDs(prefix = "", sourceIDs = {}) {
  const map = (id) => prefixedID(String(id || ""), prefix);
  return {
    prompt: map(sourceIDs.prompt || "prompt"),
    status: map(sourceIDs.status || "composer-status"),
    send: map(sourceIDs.send || "send-button"),
    stop: map(sourceIDs.stop || "stop-button"),
    attachments: map(sourceIDs.attachments || "composer-attachments"),
    fileRefs: map(sourceIDs.fileRefs || "composer-file-refs")
  };
}

// mountComposerTemplate 克隆同一份模板并重写 id 引用。prefix="" 给主会话，
// 子代理传入其稳定会话 key；调用方可从返回值直接拿真实控件，不靠再次拼 id。
export function mountComposerTemplate(host, prefix = "") {
  if (!host) return null;
  const template = document.getElementById("composer-template");
  if (!template || template.tagName !== "TEMPLATE") {
    throw new Error("缺少 #composer-template：输入框结构必须由唯一模板提供");
  }
  const fragment = template.content.cloneNode(true);
  rekeyComposer(fragment, prefix);
  host.replaceChildren(fragment);
  const ids = composerControlIDs(prefix);
  return {
    form: host,
    prompt: host.querySelector(`#${CSS.escape(ids.prompt)}`),
    status: host.querySelector(`#${CSS.escape(ids.status)}`),
    send: host.querySelector(`#${CSS.escape(ids.send)}`),
    stop: host.querySelector(`#${CSS.escape(ids.stop)}`),
    prefix
  };
}

// composerTemplateControls 供静态门禁复用：结构节点和 id 仍只来自模板。
export function composerTemplateControls() {
  const template = document.getElementById("composer-template");
  if (!template) return null;
  return {
    prompt: template.content.querySelector("#prompt"),
    status: template.content.querySelector("#composer-status"),
    send: template.content.querySelector("#send-button")
  };
}
