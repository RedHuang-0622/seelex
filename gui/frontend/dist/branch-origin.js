import { sessionForkOrigin } from "./tree-fork.js";

// branchOriginModel 把子会话的 forked_from 与权威目录行折成顶部血缘条模型。
// 父标题只用于展示；返回入口始终使用 forked_from.parent_session_id。
export function branchOriginModel(forkedFrom, sessions = []) {
  const origin = sessionForkOrigin(forkedFrom);
  if (!origin) return null;
  const parent = (Array.isArray(sessions) ? sessions : []).find(session => session?.id === origin.parentID);
  return {
    ...origin,
    parentName: String(parent?.name || "").trim() || origin.parentID,
    returnLabel: `返回父会话 ${String(parent?.name || "").trim() || origin.parentID}`
  };
}
