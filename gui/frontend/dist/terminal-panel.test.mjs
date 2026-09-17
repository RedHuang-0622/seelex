import assert from "node:assert/strict";
import test from "node:test";

import {
  TERMINAL_DEFAULT_HEIGHT,
  TERMINAL_MIN_HEIGHT,
  clampTerminalHeight,
  decodeBase64Bytes,
  encodeBase64Bytes,
  nextActiveTerminal,
  normalizeTerminalState,
  terminalEventOf,
  terminalTabItems
} from "./terminal-panel.js";

test("clampTerminalHeight keeps the panel inside [min, viewport ratio]", () => {
  assert.equal(clampTerminalHeight(0, 1000), TERMINAL_MIN_HEIGHT);
  assert.equal(clampTerminalHeight(-40, 1000), TERMINAL_MIN_HEIGHT);
  assert.equal(clampTerminalHeight(300, 1000), 300);
  // 1000 * 0.72 = 720 → 上限
  assert.equal(clampTerminalHeight(5000, 1000), 720);
  // 视口很矮时上限不得低于最小值（否则面板会被钳成 0）
  assert.equal(clampTerminalHeight(5000, 100), TERMINAL_MIN_HEIGHT);
  // 非法输入回退默认高度
  assert.equal(clampTerminalHeight(Number.NaN, 1000), TERMINAL_DEFAULT_HEIGHT);
  assert.equal(clampTerminalHeight(300, 0), 300);
});

test("normalizeTerminalState rejects external garbage from localStorage", () => {
  assert.deepEqual(normalizeTerminalState(null, 900), { open: false, collapsed: false, height: TERMINAL_DEFAULT_HEIGHT });
  assert.deepEqual(normalizeTerminalState("nope", 900), { open: false, collapsed: false, height: TERMINAL_DEFAULT_HEIGHT });
  assert.deepEqual(normalizeTerminalState({ open: "yes", collapsed: 1, height: "x" }, 900), {
    open: false,
    collapsed: false,
    height: TERMINAL_DEFAULT_HEIGHT
  });
  assert.deepEqual(normalizeTerminalState({ open: true, collapsed: true, height: 320 }, 900), {
    open: true,
    collapsed: true,
    height: 320
  });
  assert.equal(normalizeTerminalState({ height: 99_999 }, 900).height, 648);
});

test("terminalTabItems numbers duplicate shells and marks the active one", () => {
  const items = terminalTabItems([
    { id: "term-1", title: "powershell", running: true },
    { id: "term-2", title: "powershell", running: true },
    { id: "term-3", title: "bash", running: false }
  ], "term-2");
  assert.deepEqual(items.map(item => item.label), ["powershell", "powershell 2", "bash"]);
  assert.deepEqual(items.map(item => item.active), [false, true, false]);
  assert.deepEqual(items.map(item => item.exited), [false, false, true]);
  // 缺失标题回退到 id，仍是稳定标签
  assert.equal(terminalTabItems([{ id: "term-9" }], "term-9")[0].label, "term-9");
  assert.deepEqual(terminalTabItems(null, ""), []);
});

test("nextActiveTerminal picks the right neighbour, then the left one", () => {
  const tabs = [{ id: "a" }, { id: "b" }, { id: "c" }];
  assert.equal(nextActiveTerminal(tabs, "b", "b"), "c");
  assert.equal(nextActiveTerminal(tabs, "c", "c"), "b");
  assert.equal(nextActiveTerminal(tabs, "a", "c"), "a");
  assert.equal(nextActiveTerminal(tabs, "a", "a"), "b");
  assert.equal(nextActiveTerminal([{ id: "only" }], "only", "only"), "");
  assert.equal(nextActiveTerminal(null, "a", "a"), "");
});

test("base64 helpers round-trip multibyte input unchanged", () => {
  const text = "PS G:\\Program\\go\\seelex> echo 中文🙂\r\n";
  const bytes = decodeBase64Bytes(encodeBase64Bytes(text));
  assert.equal(new TextDecoder().decode(bytes), text);
  assert.equal(decodeBase64Bytes("").length, 0);
  assert.equal(decodeBase64Bytes(undefined).length, 0);
});

test("terminalEventOf normalizes output and exit payloads", () => {
  const output = terminalEventOf({ id: "term-1", kind: "output", data: encodeBase64Bytes("hi") });
  assert.equal(output.kind, "output");
  assert.equal(new TextDecoder().decode(output.bytes), "hi");
  assert.equal(output.exitCode, undefined);

  const exit = terminalEventOf({ id: "term-1", kind: "exit", exit_code: 3 });
  assert.equal(exit.kind, "exit");
  assert.equal(exit.exitCode, 3);
  assert.equal(exit.bytes.length, 0);

  const unknownCode = terminalEventOf({ id: "term-1", kind: "exit" });
  assert.equal(unknownCode.exitCode, null);

  // 缺 id 的事件无法归属到任何终端：直接丢弃，不猜
  assert.equal(terminalEventOf({ kind: "output", data: "aGk=" }), null);
  assert.equal(terminalEventOf(null), null);
});
