import assert from "node:assert/strict";
import test from "node:test";

import { branchOriginModel } from "./branch-origin.js";

test("branch origin uses forked_from and keeps parent id as the return key", () => {
  const model = branchOriginModel({
    parent_session_id: "parent-id",
    fork_point: { event_seq: 12 },
    source_event_seq: 13
  }, [{ id: "parent-id", name: "父会话标题" }]);
  assert.deepEqual(model, {
    parentID: "parent-id",
    forkPoint: 12,
    sourceEventSeq: 13,
    label: "从 parent-id 分叉",
    parentName: "父会话标题",
    returnLabel: "返回父会话 父会话标题"
  });
});

test("non-forked sessions do not invent lineage from list order", () => {
  assert.equal(branchOriginModel(null, [{ id: "some-parent" }]), null);
  assert.equal(branchOriginModel({}, [{ id: "some-parent" }]), null);
});
