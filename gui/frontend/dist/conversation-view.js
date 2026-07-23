const BOTTOM_THRESHOLD = 72;

export function createConversationView(container, options = {}) {
  const htmlByKey = new Map();
  let payloads = new Map();
  let followsTail = true;
  container.addEventListener("scroll", () => { followsTail = isNearBottom(container); }, { passive: true });
  container.addEventListener("click", event => handleAction(event, () => payloads, options));

  return {
    render(model, options = {}) {
      const before = scrollState(container, followsTail);
      payloads = model.payloads;
      reconcile(container, model.items, htmlByKey, payloads);
      restoreScroll(container, before, options.scrollMode || "auto");
      followsTail = isNearBottom(container);
    },
    payload(key) { return payloads.get(key) || ""; }
  };
}

async function handleAction(event, getPayloads, options) {
  const copyButton = event.target.closest("[data-copy]");
  if (copyButton) {
    const value = getPayloads().get(copyButton.dataset.copy) || "";
    try {
      await options.copyText(value);
      options.notify("已复制");
    } catch (error) { options.notify(error); }
    return;
  }
  const expandButton = event.target.closest("[data-expand]");
  if (!expandButton) return;
  const panel = expandButton.closest(".io-panel");
  const value = getPayloads().get(expandButton.dataset.expand) || "";
  panel?.querySelector("pre")?.replaceChildren(document.createTextNode(value));
  panel?.classList.add("expanded");
  expandButton.remove();
}

function reconcile(container, items, htmlByKey, payloads) {
  const existing = new Map([...container.children].map(node => [node.dataset.conversationKey, node]));
  const desired = new Set();
  let cursor = container.firstElementChild;
  for (const item of items) {
    desired.add(item.key);
    let node = existing.get(item.key);
    if (!node || htmlByKey.get(item.key) !== item.html) {
      const replacement = elementFromHTML(item.html);
      if (node) {
        const wasCursor = node === cursor;
        const uiState = captureUIState(node);
        node.replaceWith(replacement);
        restoreUIState(replacement, uiState, payloads);
        if (wasCursor) cursor = replacement;
      }
      node = replacement;
      htmlByKey.set(item.key, item.html);
    }
    if (node !== cursor) container.insertBefore(node, cursor);
    cursor = node.nextElementSibling;
  }
  while (cursor) {
    const next = cursor.nextElementSibling;
    htmlByKey.delete(cursor.dataset.conversationKey);
    cursor.remove();
    cursor = next;
  }
  for (const key of [...htmlByKey.keys()]) if (!desired.has(key)) htmlByKey.delete(key);
}

function captureUIState(node) {
  return {
    details: [...node.querySelectorAll("details")].map(details => details.open),
    expanded: new Set([...node.querySelectorAll(".io-panel.expanded")].map(panel => panel.dataset.payload))
  };
}

function restoreUIState(node, state, payloads) {
  [...node.querySelectorAll("details")].forEach((details, index) => {
    if (state.details[index] !== undefined) details.open = state.details[index];
  });
  for (const panel of node.querySelectorAll(".io-panel")) {
    if (!state.expanded.has(panel.dataset.payload)) continue;
    panel.querySelector("pre")?.replaceChildren(document.createTextNode(payloads.get(panel.dataset.payload) || ""));
    panel.classList.add("expanded");
    panel.querySelector("[data-expand]")?.remove();
  }
}

function elementFromHTML(html) {
  const template = document.createElement("template");
  template.innerHTML = html.trim();
  return template.content.firstElementChild;
}

function scrollState(container, followsTail) {
  return {
    top: container.scrollTop,
    height: container.scrollHeight,
    followsTail: followsTail || isNearBottom(container)
  };
}

function restoreScroll(container, before, mode) {
  if (mode === "bottom" || (mode === "auto" && before.followsTail)) {
    container.scrollTop = container.scrollHeight;
    return;
  }
  if (mode === "anchor") {
    container.scrollTop = before.top + Math.max(container.scrollHeight - before.height, 0);
  }
}

function isNearBottom(container) {
  return container.scrollHeight - container.scrollTop - container.clientHeight <= BOTTOM_THRESHOLD;
}
