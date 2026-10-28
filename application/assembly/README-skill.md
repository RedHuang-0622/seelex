# assembly/skill（根包分卷）

## 生态位

Skill 指令信封编解码

覆盖：`skill*.go`；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### skill_context.go

- `func WrapModelInput(displayInput, body string) string`
- `func DisplayUserInput(modelInput string) string`
- `func parseModelEnvelope(input string) (string, int, bool)`
- `func inputIndexUserText(message model.Message) string` — inputIndexUserText returns the display text for a user input row. Non-user
