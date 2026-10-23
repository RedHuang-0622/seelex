package main

// select.go —— 传输选择的**纯函数**：输入项目状态，输出决策，不做任何 IO。
//
// 纯函数是刻意要求：选择判据一旦掺进网络探测或文件读取，就既难单测也难解释
// （"为什么这次走了本机"变成玄学）。这里只回答"给定状态，该走哪一面"。

// ProjectState 是判据的全部输入。
type ProjectState struct {
	// Bound 表示当前会话/草稿是否绑定了项目。
	Bound bool
	// HasEndpoint 表示该项目是否配了 SSH 端点（host/port/user）。
	HasEndpoint bool
	// SSHEnabled 表示该项目是否显式开启了"用 SSH 执行"（缺省 false）。
	SSHEnabled bool
	// ProbeClass 是最近一次连通性测试的分类（"" = 从未探过；"ok" = 通）。
	ProbeClass string
}

// Decision 是选择结果，会随工具结果一起进轨迹。
type Decision struct {
	Transport string
	Reason    string
	Degraded  bool
	Detail    string
}

// Resolve 是六格判据 + 显式意图优先。
//
// 两条硬规矩：
//   - **隐式选择可以降级，显式选择不许降级**（显式指名要的就是那一面，
//     回落等于偷偷改了执行位置——那正是"改错文件"这类事故的形状）；
//   - **降级必须留痕**（Reason/Detail 非空），否则事后无法归因。
func Resolve(state ProjectState, explicit string) (Decision, error) {
	switch explicit {
	case "local":
		return Decision{Transport: "local", Reason: "explicit_local"}, nil
	case "ssh":
		// 显式要远端：配置不全就是错，静默回落成本机是更坏的结果。
		if !state.HasEndpoint {
			return Decision{}, toolErr(CodeBadArg, "先给项目配置 SSH 端点",
				"显式指定 ssh 执行面，但该项目没有 SSH 端点")
		}
		if !state.SSHEnabled {
			return Decision{}, toolErr(CodeBadArg, "先在项目设置里开启\"用 SSH 执行\"",
				"显式指定 ssh 执行面，但该项目未启用 SSH")
		}
		return Decision{Transport: "ssh", Reason: "explicit_ssh"}, nil
	case "":
		// 走到下面的自适应矩阵
	default:
		return Decision{}, toolErr(CodeBadArg, "transport 只接受 local/ssh",
			"未知执行面：%s", explicit)
	}

	switch {
	case !state.Bound:
		return Decision{Transport: "local", Reason: "no_project"}, nil
	case !state.HasEndpoint:
		return Decision{Transport: "local", Reason: "no_ssh_endpoint"}, nil
	case !state.SSHEnabled:
		return Decision{Transport: "local", Reason: "ssh_disabled"}, nil
	case state.ProbeClass == "ok":
		return Decision{Transport: "ssh", Reason: "probe_ok"}, nil
	case state.ProbeClass == "":
		// 没探过不等于通：默认本机，但把"为什么"写清楚。
		return Decision{
			Transport: "local", Reason: "not_probed", Degraded: true,
			Detail: "SSH 已启用但尚未做连通性测试",
		}, nil
	default:
		return Decision{
			Transport: "local", Reason: "probe_failed", Degraded: true,
			Detail: "最近一次连通性测试失败：" + state.ProbeClass,
		}, nil
	}
}
