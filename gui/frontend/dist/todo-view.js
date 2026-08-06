import { escapeHtml } from "./components.js";

// ── todolist 待办面板（工作台可视化）──────────────────────────────
// 数据源：snapshot.runtime.todo_items（权威 Snapshot / runtime.changed
// 增量投影，主代理每次 todolist_* 工具完成刷新）。渲染只读展示，不维护
// 本地猜测状态；所有文本 escape。

// todoView 归一化待办条目（防御畸形载荷：非数组 → []；
// 非对象或 text 非字符串的条目丢弃）。
export function todoView(items) {
  if (!Array.isArray(items)) return [];
  return items
    .filter(item => isTodoItem(item) && typeof item?.text === "string")
    .map(item => ({ text: item.text, done: Boolean(item.done) }));
}

// renderTodoList 渲染待办清单 HTML（列表 + 进度摘要）。
// 进度与条目状态均来自权威 JSON 的 done 标志，不做本地推导。
export function renderTodoList(items) {
  const list = todoView(items);
  const done = list.reduce((count, item) => count + (item.done ? 1 : 0), 0);
  const percent = list.length ? Math.round((done / list.length) * 100) : 0;
  return `<div class="todo-summary">
      <span>${done} / ${list.length} 完成</span>
      <span class="todo-progress" role="progressbar" aria-label="Todo progress" aria-valuemin="0" aria-valuemax="100" aria-valuenow="${percent}">${percent}%</span>
    </div>
    <ol class="todo-list">${list.map((item, index) => `
      <li class="todo-item${item.done ? " is-done" : ""}" data-todo-index="${index}">
        <span class="todo-check" aria-hidden="true">${item.done ? "✓" : "○"}</span>
        <span class="todo-text" title="${escapeHtml(item.text)}">${escapeHtml(item.text)}</span>
      </li>`).join("") || '<li class="todo-empty">暂无待办项</li>'}
    </ol>`;
}

function isTodoItem(value) {
  return Boolean(value) && typeof value === "object";
}
