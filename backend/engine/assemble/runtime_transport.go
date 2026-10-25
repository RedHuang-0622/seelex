package assemble

// runtime_transport.go —— 传输选择中间件的**生产装配**（组合根这一层才认识项目、
// 存储与 ssh 包；registry 只认识函数值端口）。
//
// 接进来的东西：
//   - 会话 → 项目（工作区 ID）：复用 Runtime 已有的会话工作区映射（按会话，不按进程）；
//   - 项目 → SSH 端点：读 contract.WorkspaceConnectionPort（存储侧实现后自动点亮；
//     没实现 = 该项目"未配连接" = 本地执行，与今天逐字一致）；
//   - 探针：backend/tool/ssh 的带缓存探针（TTL 内白读，未探不替人探）；
//   - 建连面：ssh.Backend（一次一连接，认证走 agent/key 的 auto 优先级）。
//
// 凭据面的缺口（如实记录，不在本层假装补上）：连接配置的**读面永不返回口令**
// （contract/workspace_connection.go 的语义约束），所以本层构造的 Spec 只带
// Auth=auto（能用的只有 ssh-agent 与默认私钥位置）。口令认证要等凭据读面（
// 从 OS 凭据库取引用）接上，届时只改这里的一行 Spec 构造，选择规则一行不动。
//
// 消费者名单 = 五个**真的有远端面**的工具（read_file / write_file / edit_file /
// bash / bg_manage，名单见 toolreg.RemoteFaceToolNames）。闸门与消费点同一个提交里
// 改：选择器把 ssh 写进 ctx 而 handler 不读，等于"说走远端其实跑了本地"，比不选更
// 危险。其余工具（grep_search / glob / read_batch / bash_bg）仍只有本地面，因此不在
// 名单里——它们连"这次走哪儿"都不判断，链上逐字等于旧行为。

import (
	"context"
	"os"
	"strings"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
	toolreg "github.com/RedHuang-0622/seelex/backend/tool/registry"
	seelessh "github.com/RedHuang-0622/seelex/backend/tool/ssh"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	seetelemetry "github.com/RedHuang-0622/seelex/internal/telemetry"
)

// sessionWorkspaceID 返回会话绑定的工作区 ID（未绑定 = ""，即"没有项目归属"）。
//
// 键是会话：同一进程里两个会话各自绑定自己的项目，传输选择因此不会互相污染
// （并发矩阵用例钉的就是这条口径）。
func (r *Runtime) sessionWorkspaceID(sessionID string) string {
	if r == nil || sessionID == "" {
		return ""
	}
	r.sessionWorkspacesMu.RLock()
	defer r.sessionWorkspacesMu.RUnlock()
	return r.sessionWorkspaces[sessionID]
}

// transportSelector 装配传输中间件（**两段共用一份**；见 registry 的 transport_decision.go）。
//
// 装配进链的位置由 NewRegistryState 定，两处都是这一份：
//
//	事件 → **决策段（只读）** → 路径门 → 权限门 → **建连段（迟绑定）** → 诊断 → handler
//
// 决策段因此会**按调用**读一次项目连接配置与探针缓存（都是读面，不发网络包）；真正的
// 探测与拨号仍在权限门之后——被拒的调用不白探、不白连。
func (r *Runtime) transportSelector() frameworktools.MetaMiddleware {
	prober := seelessh.NewProber(seelessh.DefaultProbeTTL, nil)
	deps := toolreg.TransportSelectorDeps{
		Enabled:   transportEnabled(),
		Consumers: toolreg.TransportConsumers(toolreg.RemoteFaceToolNames()...),
		SessionKey: func(ctx context.Context) string {
			return seetelemetry.SessionIDFromContext(ctx)
		},
		Project: func(ctx context.Context, sessionID string) string {
			if binding, ok := toolreg.TransportBindingFromContext(ctx); ok && binding.ProjectID != "" {
				return binding.ProjectID
			}
			return r.sessionWorkspaceID(sessionID)
		},
		Endpoint:       r.sshEndpointForProject,
		EnvironmentFor: r.transportEnvironmentForEnv,
		EnvFor: func(ctx context.Context, sessionID string) string {
			if binding, ok := toolreg.TransportBindingFromContext(ctx); ok {
				return binding.Env
			}
			return r.sessionSSHEnvFor(sessionID)
		},
		Probe:    proberVerdict(prober),
		ProbeNow: probeNow(prober),
		Dial:     dialSSH,
	}
	return toolreg.TransportSelector(deps)
}

// transportEnabled 报告这台宿主是否具备 SSH 执行能力（装配级开关）。
//
// 缺省开启：能力"存在"与"可用"是两件事——可用性由探针逐项目回答，而这一个开关
// 回答的是"这台宿主有没有这条路"。用 env 而不是配置项：它是宿主能力，不是产品
// 偏好（与 obs.RequestLogEnv 同类口径）。
func transportEnabled() bool {
	switch os.Getenv("SEELEX_SSH_TRANSPORT") {
	case "off", "0", "false":
		return false
	}
	return true
}

// sshEndpointForProject 读项目连接配置里的 SSH 端点（口令永远读不到，也不需要）。
func (r *Runtime) sshEndpointForProject(projectID string) (toolreg.SSHEndpoint, bool, error) {
	port := r.workspaceConnectionPort()
	if port == nil {
		// 没装配连接读面：等价于"未配连接"（本地执行，与今天一致）。
		return toolreg.SSHEndpoint{}, false, nil
	}
	connection, err := port.WorkspaceConnection(projectID)
	if err != nil {
		return toolreg.SSHEndpoint{}, false, err
	}
	return sshEndpoint(connection)
}

// transportEnvironmentForEnv 按会话固定环境名读项目环境表里的执行面。
//
// 地址为空的环境是 local（例如 dev）；有端点才是 ssh（test/prod）。查不到仍返回
// ok=false，绝不回落单条连接/默认环境。
func (r *Runtime) transportEnvironmentForEnv(projectID, env string) (toolreg.TransportEnvironment, bool, error) {
	port := r.workspaceEnvironmentPort()
	if port == nil || strings.TrimSpace(env) == "" {
		return toolreg.TransportEnvironment{}, false, nil
	}
	list, _, err := port.WorkspaceEnvironments(projectID)
	if err != nil {
		return toolreg.TransportEnvironment{}, false, err
	}
	for _, item := range list {
		if item.Name != env {
			continue
		}
		host, user := strings.TrimSpace(item.Host), strings.TrimSpace(item.User)
		if host == "" && user == "" {
			return toolreg.TransportEnvironment{Name: item.Name, Kind: toolreg.TransportLocal}, true, nil
		}
		if host == "" || user == "" {
			return toolreg.TransportEnvironment{}, false, nil
		}
		return toolreg.TransportEnvironment{
			Name: item.Name, Kind: toolreg.TransportSSH,
			Endpoint: toolreg.SSHEndpoint{
				Host: host, Port: item.Port, User: user, RemoteRoot: item.RemoteRoot, Name: item.Name,
			},
		}, true, nil
	}
	return toolreg.TransportEnvironment{}, false, nil
}

// SetSessionSSHEnvResolver 注入"会话当前环境"读面（装配期调用一次；nil = 没有缺省）。
//
// 与 SetWorkspaceEnvironmentPort 分开注入：前者按会话取，后者按项目取，两者的
// 事实源不同（会话状态 / 项目环境表），混成一个注入点会让"谁覆盖谁"失去依据。
func (r *Runtime) SetSessionSSHEnvResolver(resolver func(sessionID, projectID string) string) {
	if r == nil {
		return
	}
	r.sessionSSHEnvMu.Lock()
	defer r.sessionSSHEnvMu.Unlock()
	r.sessionSSHEnv = resolver
}

// sessionSSHEnvFor 读会话当前限定的环境名（空 = 没有缺省）。
func (r *Runtime) sessionSSHEnvFor(sessionID string) string {
	if r == nil || sessionID == "" {
		return ""
	}
	r.sessionSSHEnvMu.RLock()
	resolver := r.sessionSSHEnv
	r.sessionSSHEnvMu.RUnlock()
	if resolver == nil {
		return ""
	}
	projectID := ""
	if resolve := r.sessionWorkspaceFor(sessionID); resolve != nil {
		projectID = resolve()
	}
	return resolver(sessionID, projectID)
}

// SetWorkspaceEnvironmentPort 注入项目连接/环境表读面（装配期调用一次）。
//
// 一个端口两个用途：`WorkspaceConnection` 给"没指名环境"的兜底，
// `WorkspaceEnvironments` 给按环境名解析。两者在 adapters.WorkspacePort 上是
// 同一个实现，分开注入只会让两处读到不同实例（缓存各一份 = 判定链前后不一致）。
func (r *Runtime) SetWorkspaceEnvironmentPort(port contract.WorkspaceEnvironmentPort) {
	if r == nil {
		return
	}
	r.workspacePortMu.Lock()
	defer r.workspacePortMu.Unlock()
	r.workspacePort = port
}

// workspaceEnvironmentPort 读注入的环境表读面（nil = 未装配）。
func (r *Runtime) workspaceEnvironmentPort() contract.WorkspaceEnvironmentPort {
	if r == nil {
		return nil
	}
	r.workspacePortMu.RLock()
	defer r.workspacePortMu.RUnlock()
	return r.workspacePort
}

// workspaceConnectionPort 读注入的连接读面（同一份端口的窄视图）。
func (r *Runtime) workspaceConnectionPort() contract.WorkspaceConnectionPort {
	port := r.workspaceEnvironmentPort()
	if port == nil {
		return nil
	}
	connection, _ := any(port).(contract.WorkspaceConnectionPort)
	return connection
}

// sshEndpoint 把连接配置折成选择器要的窄形状。
//
// 远端项目根（SSHEndpoint.RemoteRoot）从连接配置的 SSH.RemoteRoot 折出来
// （2026-10-2x）：那一格填了，路径门在远端面上就按它判"根内 / 越界"；没填（空 =
// 未知）就退回更保守的口径——把"本地根也容不下的绝对路径"全算成越界候选，交给
// SSH 路径白名单过问。两种情形都不猜根，判据只有一处（path_gate_face.go）。
func sshEndpoint(connection model.WorkspaceConnection) (toolreg.SSHEndpoint, bool, error) {
	if connection.SSH == nil || connection.SSH.Host == "" {
		return toolreg.SSHEndpoint{}, false, nil
	}
	return toolreg.SSHEndpoint{
		Host: connection.SSH.Host, Port: connection.SSH.Port, User: connection.SSH.User,
		RemoteRoot: connection.SSH.RemoteRoot,
	}, true, nil
}

// proberVerdict 把探针缓存的读面接到选择器（未探 = ok=false，选择器不替人探）。
func proberVerdict(prober *seelessh.Prober) func(context.Context, string, toolreg.SSHEndpoint) (toolreg.TransportProbeVerdict, bool) {
	return func(_ context.Context, projectID string, endpoint toolreg.SSHEndpoint) (toolreg.TransportProbeVerdict, bool) {
		result, ok := prober.Cached(sshSpecFor(projectID, endpoint))
		if !ok {
			return toolreg.TransportProbeVerdict{}, false
		}
		return probeVerdict(result), true
	}
}

// probeNow 是"显式指名者付的探测费"。
func probeNow(prober *seelessh.Prober) func(context.Context, string, toolreg.SSHEndpoint) toolreg.TransportProbeVerdict {
	return func(ctx context.Context, projectID string, endpoint toolreg.SSHEndpoint) toolreg.TransportProbeVerdict {
		return probeVerdict(prober.ProbeNow(ctx, sshSpecFor(projectID, endpoint)))
	}
}

// probeVerdict 把探针结论翻成选择器词表（分类词表只有 ssh 包一份）。
func probeVerdict(result seelessh.ProbeResult) toolreg.TransportProbeVerdict {
	verdict := toolreg.TransportProbeVerdict{
		State: toolreg.TransportProbeFailed, Class: result.Class, Detail: result.Hint,
	}
	if result.OK {
		verdict.State = toolreg.TransportProbeOK
	}
	return verdict
}

// dialSSH 是建连面：只在探针通过之后被调用。
func dialSSH(_ context.Context, projectID string, endpoint toolreg.SSHEndpoint) (toolreg.Transport, error) {
	backend := seelessh.NewBackend(sshSpecFor(projectID, endpoint))
	return toolreg.NewSSHTransport(backend, endpoint.Addr()), nil
}

// sshSpecFor 由端点折出一次连接的 Spec。
//
// 认证只给 auto（agent → 默认私钥）：口令在凭据面，读面按契约不返回明文（见文件头的
// 凭据缺口说明）。KnownHosts 恒为 strict——首次连接必须先由显式动作信任。
func sshSpecFor(projectID string, endpoint toolreg.SSHEndpoint) seelessh.Spec {
	return seelessh.Spec{
		ID: projectID, Hostname: endpoint.Host, Port: endpoint.Port, User: endpoint.User,
		Auth:       seelessh.AuthSpec{Mode: seelessh.AuthAuto},
		KnownHosts: seelessh.KnownHostsSpec{Mode: seelessh.KnownHostsStrict},
	}
}
