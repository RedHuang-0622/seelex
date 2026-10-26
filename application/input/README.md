# application/input

## 生态位

`application/input` 承载输入受理门禁、输入框建议塑形与输入前缀词法。主要调用方是
`application/compat.Frontend` 的 `Submit` / `Suggestions`，以及根门面的
`HasSigilPrefix` / `SigilOf` 兼容函数。

## 职责

- 定义输入前缀与建议域词表。
- 统一 closed/draining 受理门禁、空白裁剪与 dispatcher 委托。
- 组合命令、Skill、Plugin 三个权威目录的候选。
- 按前缀过滤，并保持 command → skill → plugin 的稳定排序。
- 提供 `HasSigilPrefix` / `SigilOf`；根 `application` 门面直接再导出这两个
  词法函数，`application/core` 不再保留同义包装。
- 参数区或未知前缀返回空建议。

## 非职责

- 不执行命令、Skill 或 Plugin。
- 不读取命令、Skill、Plugin 目录。
- 不解析普通对话，也不拥有 chat 状态。
- 不导入 `application/core`。

## 文件结构

- `suggestions.go`：sigil 解析、`Suggestion` DTO、`Deps` 和候选塑形。
- `submission.go`：`SubmissionApplication`，只持生命周期读回调和 dispatcher，不拥
  有命令、Skill 或 Conversation 路由。
- `command/`：基础内置命令注册与 `contract.CommandResult` 适配。
- `suggestions_test.go`：跨域排序、过滤和参数区拒绝。

## 核心实现

`Deps` 只提供三个目录读取函数。`Application` 无状态，不缓存目录；每次建议请求
读取调用方注入的当前目录值。

## 数据流

根门面 `HasSigilPrefix` / `SigilOf` 和 `Frontend.Suggestions` →
`Application.Suggestions` → 命令/Skill/Plugin 目录 → 过滤与排序 →
前端输入建议。

`Frontend.Submit` → `SubmissionApplication.Submit` → closed/draining 门禁 →
`contract.InputDispatcher`。路由判据仍在 dispatcher，core 不再保留公开 `Submit`
方法。

## 依赖方向

只依赖标准库。禁止依赖 `application/core`、backend、middle 或 UI。

## 并发与错误

纯读取、无内部锁；目录读取的并发语义由注入端口拥有。非法输入返回 nil，不产生
错误或副作用。

## 扩展方式

新增建议域时先冻结 sigil 与目录端口，再显式加入稳定优先级；不得在 UI 复制排序表。

## Review 指南

- sigil 词表是否只有一份。
- `/` 是否仍只列命令与 Skill，不泄漏模型工具。
- 参数区是否不触发目录读取。
- 返回切片是否与调用方目录解耦。

## 测试与验证

```text
go test ./application/input ./application/core -run 'Suggestions|Sigil' -count=1
go test ./application/... -count=1
```
