// 性能钩子的两处口径：
//   ① domNodes 必须走 live HTMLCollection（getElementsByTagName），不能每次
//      querySelectorAll("*") 全量遍历 + 装数组 —— 它被 10s 轮询与每次
//      markRender 调用，属于"诊断本身在制造卡顿"的那类成本；
//   ② start()/stop() 幂等，stop() 清掉轮询定时器（视图重建/重连不叠表）。
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { createPerfHooks } from "./perf-hooks.js";

function fakeElement() {
  return {
    className: "",
    textContent: "",
    title: "",
    attrs: {},
    children: [],
    setAttribute(name, value) { this.attrs[name] = value; },
    addEventListener() {},
    appendChild(child) { this.children.push(child); return child; }
  };
}

function withFakeDocument(run, { nodes = 0 } = {}) {
  const previous = globalThis.document;
  const dom = {
    nodes,
    createElement: () => fakeElement(),
    getElementsByTagName: () => ({ length: dom.nodes }),
    querySelectorAll: () => { throw new Error("domNodes 不应再走 querySelectorAll"); }
  };
  globalThis.document = dom;
  try { return run(dom); } finally { globalThis.document = previous; }
}

test("domNodes 走 live 集合：只读 length，不做全量查询", async () => {
  const source = await readFile(new URL("./perf-hooks.js", import.meta.url), "utf8");
  assert.match(source, /getElementsByTagName\("\*"\)/);
  assert.doesNotMatch(source, /querySelectorAll\("\*"\)/);

  withFakeDocument((dom) => {
    const perf = createPerfHooks({});
    dom.nodes = 4321;
    const sample = perf.record({ renderMs: 3 });
    assert.equal(sample.domNodes, 4321, "节点数应来自 live 集合的当前值");
    assert.equal(perf.badge.textContent.includes("4321"), true, "徽标应显示节点数");
  });
});

test("start/stop 幂等：不叠定时器，stop 清句柄", () => {
  const previousSet = globalThis.setInterval;
  const previousClear = globalThis.clearInterval;
  const setCalls = [];
  const clearCalls = [];
  globalThis.setInterval = (fn, ms) => { const handle = { id: setCalls.length + 1, fn, ms }; setCalls.push(handle); return handle; };
  globalThis.clearInterval = (handle) => { clearCalls.push(handle); };
  try {
    withFakeDocument(() => {
      const perf = createPerfHooks({});
      perf.start();
      perf.start();                       // 第二次 start 不得再建定时器
      assert.equal(setCalls.length, 1, "重复 start 只允许一个轮询");
      assert.equal(setCalls[0].ms > 0, true);

      perf.stop();
      assert.equal(clearCalls.length, 1);
      assert.equal(clearCalls[0], setCalls[0], "清掉的必须是 start 建的那个句柄");

      perf.stop();                        // 幂等
      assert.equal(clearCalls.length, 1);

      perf.start();                       // stop 后可重新开始
      assert.equal(setCalls.length, 2);
    });
  } finally {
    globalThis.setInterval = previousSet;
    globalThis.clearInterval = previousClear;
  }
});
