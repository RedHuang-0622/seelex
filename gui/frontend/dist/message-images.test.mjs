// message-images.test.mjs — 用户消息随行图片的缩略图注水（纯逻辑，可脱 DOM 覆盖）。
//
// 这一层最该钉住的是三件事：
//   1) 同一张图（同 ref）只打一次 RPC —— 对话区每帧重渲染都会重新扫到同一批节点；
//   2) 读不回来不静默 —— 打 `missing` 标记，UI 画占位而不是永远空白；
//   3) 缓存有上限 —— 每条是一个完整 data URL，长会话不能无限吃内存。
import assert from "node:assert/strict";
import test from "node:test";

const {
  registerMessageImageLoader,
  hydrateMessageImages,
  clearMessageImageCache,
  messageImageCacheSize
} = await import(new URL("./message-images.js", import.meta.url).href);

// fakeFigure / fakeImage 是够用的最小 DOM：本模块只碰 dataset、src 与 closest。
function fakeFigure(ref) {
  return { dataset: { messageImageFigure: ref } };
}

function fakeImage(ref) {
  const figure = fakeFigure(ref);
  return {
    dataset: { messageImage: ref },
    src: "",
    closest(selector) { return selector === "[data-message-image-figure]" ? figure : null; },
    figure
  };
}

// fakeRoot 复刻用到的两个查询：全量扫描（`[data-message-image]`）与按 ref 精确匹配。
function fakeRoot(images = []) {
  return {
    images,
    querySelectorAll(selector) {
      if (selector === "[data-message-image]") return images;
      const match = /^\[data-message-image="(.+)"\]$/.exec(selector);
      if (!match) return [];
      const ref = match[1].replace(/\\(.)/g, "$1");
      return images.filter(image => image.dataset.messageImage === ref);
    }
  };
}

function stubLoader(entries) {
  const calls = [];
  const loader = async ref => {
    calls.push(ref);
    const entry = entries[ref];
    if (entry instanceof Error) throw entry;
    return entry;
  };
  return { loader, calls };
}

test("把 ref 注水成 data URL：MIME 由后端给，前缀由渲染层拼", async () => {
  clearMessageImageCache();
  const { loader, calls } = stubLoader({
    "media:aa": { mime_type: "image/png", data_base64: "cG5n" }
  });
  registerMessageImageLoader(loader);
  const root = fakeRoot([fakeImage("media:aa")]);
  hydrateMessageImages(root);
  await new Promise(resolve => setImmediate(resolve));
  assert.deepEqual(calls, ["media:aa"]);
  assert.equal(root.images[0].src, "data:image/png;base64,cG5n");
});

test("同一 ref 只打一次 RPC：重渲染（含并发注水）都走缓存", async () => {
  clearMessageImageCache();
  const { loader, calls } = stubLoader({
    "media:aa": { mime_type: "image/jpeg", data_base64: "anBn" }
  });
  registerMessageImageLoader(loader);
  const root = fakeRoot([fakeImage("media:aa"), fakeImage("media:aa")]);
  // 两次连续 hydrate（模拟同一帧内两次渲染）：在飞的那条不该被重发。
  hydrateMessageImages(root);
  hydrateMessageImages(root);
  await new Promise(resolve => setImmediate(resolve));
  assert.deepEqual(calls, ["media:aa"], `同一张图只读一次，实际 ${calls.length} 次`);
  assert.equal(root.images[1].src, "data:image/jpeg;base64,anBn", "两个节点都要被填满");

  // 再渲染一次：已就绪，直接写 src，不再发 RPC。
  const later = fakeRoot([fakeImage("media:aa")]);
  assert.equal(hydrateMessageImages(later), 1, "已缓存的节点当场填满");
  assert.equal(later.images[0].src, "data:image/jpeg;base64,anBn");
  assert.deepEqual(calls, ["media:aa"]);
});

test("读不回来的图打 missing 标记，不静默留空白", async () => {
  clearMessageImageCache();
  const { loader } = stubLoader({ "media:gone": new Error("图片不在本会话媒体分区里") });
  registerMessageImageLoader(loader);
  const root = fakeRoot([fakeImage("media:gone")]);
  hydrateMessageImages(root);
  await new Promise(resolve => setImmediate(resolve));
  const [image] = root.images;
  assert.equal(image.src, "", "读失败不写 src（不画破图）");
  assert.equal(image.dataset.messageImageState, "missing");
  assert.equal(image.figure.dataset.messageImageState, "missing");
  assert.match(image.figure.title, /图片不在本会话媒体分区里/);

  // 失败也进缓存：同一张坏图不反复重试（否则每帧一次失败 RPC）。
  const again = fakeRoot([fakeImage("media:gone")]);
  assert.equal(hydrateMessageImages(again), 0);
  assert.equal(again.images[0].dataset.messageImageState, "missing");
});

test("没注入读图口时不发请求、不报错（预览页/未装配宿主）", () => {
  clearMessageImageCache();
  registerMessageImageLoader(null);
  const root = fakeRoot([fakeImage("media:aa")]);
  assert.equal(hydrateMessageImages(root), 0);
  assert.equal(root.images[0].src, "");
});

test("缓存有上限：长会话不会把渲染进程内存吃满", async () => {
  clearMessageImageCache();
  const entries = {};
  for (let index = 0; index < 40; index += 1) {
    entries[`media:${index}`] = { mime_type: "image/png", data_base64: "cG5n" };
  }
  const { loader } = stubLoader(entries);
  registerMessageImageLoader(loader);
  for (let index = 0; index < 40; index += 1) {
    hydrateMessageImages(fakeRoot([fakeImage(`media:${index}`)]));
    await new Promise(resolve => setImmediate(resolve));
  }
  assert.ok(messageImageCacheSize() <= 24, `缓存必须收在上限内，实际 ${messageImageCacheSize()}`);
});
