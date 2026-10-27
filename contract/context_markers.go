package contract

// context_markers.go —— 引擎历史里**内部上下文行**的机器前缀（唯一一份）。
//
// 前缀有两个消费面：写入侧（application/assembly 的预算收尾输入）与判定侧
// （backend/engine/context 的内部事件过滤）。两处各写一份字面量就会漂移——
// 改了一边，另一边会把内部行当成正常对话喂回模型。契约层零依赖，放在这里
// 让两个方向都能引用而不成环。

// ReactBudgetFinalizationMarker 标记"预算耗尽后的 +1 次收尾调用"注入行。
const ReactBudgetFinalizationMarker = "<!-- seelex:react-budget-finalize:v1 -->"
