# 2026-09-13 Seele 多模态改造工作包

一次性工作包，围绕「图片上传交互 + 限流与并发」的 Seele/seelex 双侧改造。

- [`plan.md`](plan.md)：方案正文。含事实基线（file:line）、引用式 content parts
  接口契约、三前端上传交互、限额四轴与超限阶梯、加权租约与 RPM/TPM 限流、
  分阶段落地与验收、风险与退路。

状态：**规划（未实现）**。已实现且可验证的前置能力见
[`../../sessionstore/README.md`](../../sessionstore/README.md)（会话媒体分区）与
[`../../seelebridge/multimodal/README.md`](../../seelebridge/multimodal/README.md)
（content parts 编码 + 真机冒烟，2026-09-13 通过）。
