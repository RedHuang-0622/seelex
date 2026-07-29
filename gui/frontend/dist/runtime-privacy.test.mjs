import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const appSource = await readFile(new URL("./app.js", import.meta.url), "utf8");

test("does not render system prompt state in runtime details", () => {
  assert.doesNotMatch(appSource, /runtime\.prompt_stack/);
  assert.doesNotMatch(appSource, /\["Prompt", runtime\./);
});
