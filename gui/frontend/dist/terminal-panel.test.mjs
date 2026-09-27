import assert from "node:assert/strict";
import test from "node:test";

import {
  TERMINAL_DEFAULT_HEIGHT,
  TERMINAL_MIN_HEIGHT,
  TERMINAL_SCROLLBACK_DEFAULT,
  TERMINAL_SCROLLBACK_MAX,
  TERMINAL_SCROLLBACK_MIN,
  clampTerminalHeight,
  decodeBase64Bytes,
  encodeBase64Bytes,
  nextActiveTerminal,
  normalizeTerminalScrollback,
  normalizeTerminalState,
  readTerminalScrollback,
  terminalEventOf,
  terminalTabItems,
  writeTerminalScrollback
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

test("normalizeTerminalScrollback：非法回落默认、越界钳边界", () => {
  assert.equal(TERMINAL_SCROLLBACK_DEFAULT, 2000, "默认值是设置项的口径起点");
  assert.equal(normalizeTerminalScrollback(undefined), TERMINAL_SCROLLBACK_DEFAULT);
  assert.equal(normalizeTerminalScrollback(null), TERMINAL_SCROLLBACK_DEFAULT);
  assert.equal(normalizeTerminalScrollback(""), TERMINAL_SCROLLBACK_DEFAULT);
  assert.equal(normalizeTerminalScrollback("不是数字"), TERMINAL_SCROLLBACK_DEFAULT);
  assert.equal(normalizeTerminalScrollback(Number.NaN), TERMINAL_SCROLLBACK_DEFAULT);
  assert.equal(normalizeTerminalScrollback("5000"), 5000, "下拉框给的是字符串");
  assert.equal(normalizeTerminalScrollback(3210.7), 3210, "取整：xterm 要整数行数");
  assert.equal(normalizeTerminalScrollback(-5), TERMINAL_SCROLLBACK_MIN);
  assert.equal(normalizeTerminalScrollback(1e9), TERMINAL_SCROLLBACK_MAX);
});

test("readTerminalScrollback / writeTerminalScrollback：落盘读回，无存储环境不抛", () => {
  const store = new Map();
  const storage = {
    getItem: key => (store.has(key) ? store.get(key) : null),
    setItem: (key, value) => store.set(key, String(value))
  };
  assert.equal(readTerminalScrollback(storage), TERMINAL_SCROLLBACK_DEFAULT, "没写过 → 默认");
  assert.equal(writeTerminalScrollback(storage, "5000"), 5000);
  assert.equal(readTerminalScrollback(storage), 5000, "读回写过的值");
  assert.equal(writeTerminalScrollback(storage, "垃圾"), TERMINAL_SCROLLBACK_DEFAULT, "写面也归一");
  assert.equal(readTerminalScrollback(storage), TERMINAL_SCROLLBACK_DEFAULT);
  // 无存储环境（隐私模式等）：回落默认，不抛
  assert.equal(readTerminalScrollback(undefined), TERMINAL_SCROLLBACK_DEFAULT);
  assert.equal(readTerminalScrollback({ getItem() { throw new Error("blocked"); } }), TERMINAL_SCROLLBACK_DEFAULT);
  assert.equal(writeTerminalScrollback({ setItem() { throw new Error("blocked"); } }, 5000), 5000, "写失败仍返回生效值");
});
