# jsonobj

## 生态位

`internal/jsonobj` 是**从 LLM 原文里取 JSON 对象的容错层**：只修语法（补引号与
控制字符的转义），再取首个**括号平衡**的对象；不改字段名、不补字段、不猜 kind，
语义校验仍在各自的域（例如 goal 域的 `TLDirective.Validate()`）。

主要调用方：`seelebridge` 根包的 `runtime_goal_tl.go`（解析 ADVISOR 裁决）与
`runtime_role_prompt.go`（解析员工提示词优化结果）。

物理位置：2026-10-16 按
[plan.md](../../docs/2026-10-15-architecture-reshape/plan.md) §2.5 从
`seelebridge/json_object.go`（该文件已移除）归位到这里——`seelebridge` 顶层正在
按板块解散，本包是那一步抽出的第一块模块级叶子。

## 为什么需要它（事故来源）

2026-09-16 22:06 的真实 goal 收口回合里，ADVISOR 的裁决原文在 content 值里带了
**未转义的内层引号**。旧的"第一个 `{` 到最后一个 `}` → `json.Unmarshal`"写法因此
停在提前闭合的字符串处，解码失败被归进 B4 缺席矩阵——一条内容上已经 verdict_done
的裁决被判成"缺席（429/超时）"，goal 保持 active 并转人工。**一条裁决该不该收口，
不该由转义细节决定。**

## 依赖方向

只依赖标准库（`encoding/json`、`errors`、`fmt`、`strings`）。不依赖任何 Seelex 包，
也不被 `contract/` 反向依赖。

## 核心实现

- `DecodeLenient(raw string, out any) error`：三档依次尝试——严格 → 提取 → 修复后提取。
- `ErrNoJSONObject`：原文里找不到任何可解码对象时的哨兵错误，调用方用
  `errors.Is` 判定后给用户可读的失败文案。
- `firstBalancedJSONObject` / `jsonStringEndsAt` / `validJSONEscapeAt` /
  `writeEscapedControl`：包内私有，实现括号平衡扫描与最小转义修复。

## 测试与验证

```text
go test ./internal/jsonobj -count=1
```

用例 `jsonobj_test.go` 覆盖三档解码路径（含 2026-09-16 事故的原始形状）。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### jsonobj.go

- `func DecodeLenient(raw string, out any) error` — DecodeLenient 取 raw 里第一个平衡的 JSON 对象并解码到 out。
- `func firstBalancedJSONObject(raw string, repair bool) (string, bool)` — firstBalancedJSONObject 返回 raw 里第一个花括号平衡的 JSON 对象文本。
- `func jsonStringEndsAt(raw string, quote int) bool` — jsonStringEndsAt 判断 raw[i] 处的引号是不是字符串的结束引号：只有紧随其后的
- `func validJSONEscapeAt(raw string, i int) bool` — validJSONEscapeAt 判断 raw[i] == '\\' 是否开启一个合法 JSON 转义序列。
- `func writeEscapedControl(out *strings.Builder, c byte)` — writeEscapedControl 把 JSON 不允许的裸控制字节写成转义形式。

### jsonobj_test.go

- `func TestDecodeJSONObjectLenient(t *testing.T)` — TestDecodeJSONObjectLenient 钉住"只修语法、不改语义"的容错边界。

