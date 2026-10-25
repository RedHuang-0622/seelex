import assert from "node:assert/strict";
import test from "node:test";

import { approvalRiskLabel } from "./interaction-risk.js";

test("审批风险显示为中文风险等级，不与思考强度混用", () => {
  assert.equal(approvalRiskLabel("low"), "低风险");
  assert.equal(approvalRiskLabel("medium"), "中风险");
  assert.equal(approvalRiskLabel(" HIGH "), "高风险");
  assert.equal(approvalRiskLabel(""), "待评估");
  assert.equal(approvalRiskLabel("unknown"), "风险：unknown");
});
