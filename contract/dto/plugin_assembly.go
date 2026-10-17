package dto

import (
	"fmt"
	"strings"
)

// plugin_assembly.go — 「按会话装配的插件」在 DTO 层的**语法**口径一条。
//
// 分层写死（别在这一层判语义）：
//   - **语法**（本文件）：去首尾空白、丢弃空项、**重复声明显式拒绝**、每会话上限
//     ——写入侧与编排侧共用同一份规整，避免两条路对"什么算合法清单"给出两个答案；
//   - **语义**（名字是否存在于插件目录）：在 seelebridge 的编排入口判（那里手里
//     有插件定义）。DTO 层看不到插件目录，也不该 import 插件域。
//
// 为什么上限要在**写入侧**拦（与 ValidToolPolicy 同一理由）：登记阶段的一个"多写
// 了两个"，如果放行到运行时才被解释成"没装配"或"只装了前三个"，那登记的**事实**
// 与运行时**能解释的事实**就不是同一个集合了。超限一律显式拒绝，不静默截断。
//
// 为什么**重复**也要显式拒绝（2026-10-05，对抗复核）：修前这里静默去重，于是"重复"
// 的唯一生效口径是"静默合并"——而计划自己写的、存储层校验里也写着的是"未知名/超限/
// 重复**显式拒绝**（不静默去重）"，两边相反，且存储层那条拒绝**不可达**（上游已经
// 把重复吃掉了）。口径统一到"显式拒绝"：与"不静默排队/不静默截断"同族，而且 leader
// 写重复名单是**错**（笔误、两条路各写一半），不是一个需要被体贴成全的意图。

// MaxPluginsPerRole 是每个会话（teammate / 角色）的插件数**出厂上限**，也是配置键
// limits.plugins.per_teammate 的默认值。3 的取法：一个人身上挂三个能力包已经覆盖
// "一个主能力 + 一个补充 + 一个窄面"，再往上挂的代价（每轮常驻的技能目录字节、
// 工具面收窄的交叉判断）大于收益。
const MaxPluginsPerRole = 3

// NormalizePlugins 规整一份按会话装配的插件名清单：去首尾空白、丢弃空项（纯空白
// 项是脏数据，不是"禁用某个插件"的语义）、**重复声明显式报错**；数量超过 limit 也
// 显式报错。
//
// 返回值语义（与"空集 = 不覆盖"配套）：
//   - 输入为空 / 清完为空 → **nil**（不覆盖：工具面继承宿主当前装配 + 技能目录
//     不注入）。刻意不用空切片表达"装配了空集"——那种区分要靠指针字段，本轮不做
//     （见设计文档 §5.5）。
//   - limit <= 0 → 用出厂上限 MaxPluginsPerRole（调用方不掌握配置时用它兜底）。
func NormalizePlugins(names []string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = MaxPluginsPerRole
	}
	if len(names) == 0 {
		// 显式零值：空输入 = nil（“不覆盖”信号，见本函数注释）；门禁禁的是裸 `return nil, nil`。
		return []string(nil), nil
	}
	cleaned := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, duplicate := seen[name]; duplicate {
			return nil, fmt.Errorf("插件 %q 重复声明（显式拒绝，不静默去重）", name)
		}
		seen[name] = struct{}{}
		cleaned = append(cleaned, name)
	}
	if len(cleaned) > limit {
		return nil, fmt.Errorf(
			"每会话插件上限 %d 个（limits.plugins.per_teammate）：声明了 %d 个（%s）——显式拒绝，不静默截断",
			limit, len(cleaned), strings.Join(cleaned, ", "))
	}
	if len(cleaned) == 0 {
		// 显式零值：清完为空 = nil（同一个“不覆盖”信号），刻意不用空切片（见函数注释）。
		return []string(nil), nil
	}
	return cleaned, nil
}
