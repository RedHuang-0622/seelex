package registry

// path_face_permission_test.go —— **按面判定的两道闸门**的验收判据（ssh 工作项）。
//
// 判据清单（对应工作项的验收条）：
//
//	1. 远端根**内**的路径：不需要 ssh.userspace，放行（且不产生越界交接）。
//	2. 远端**越界** + 授予 ssh.userspace：放行，并把越界事实记进 RemotePathHandoff。
//	3. 远端越界 + **未**授予 ssh.userspace：**拒**，码 PERM_SSH_USERSPACE_REQUIRED，
//	   且 handler 一次都不跑（不静默放行到对端）。
//	4. 远端面 + 显式收回 ssh 通道位（`!ssh`）：拒，码 PERM_SSH_REQUIRED。
//	5. 本地执行面**逐字不变**：根内放行、越界仍报 "bound project"，与这两道门无关。
//	6. 两个角色并发判定互不污染（一个授予 userspace、一个没有，并行跑）。
//
// 全部纯字符串/纯内存：不碰网络、不碰 sshd、不碰文件系统。

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
	"github.com/RedHuang-0622/seelex/backend/limit/gate"
	"github.com/RedHuang-0622/seelex/backend/limit/policy"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// faceSessionKey 是测试里"本次调用属于哪个会话"的 ctx 键（判定档位/员工身份按它取）。
type faceSessionKey struct{}

// newFaceGate 造一个装配了默认权责表的权限门（判据入口与生产同一个）。
func newFaceGate(t *testing.T) *PermissionGate {
	t.Helper()
	gateway := &PermissionGate{}
	gateway.SessionFromContext = func(ctx context.Context) string {
		session, _ := ctx.Value(faceSessionKey{}).(string)
		return session
	}
	gateway.Set(policy.DefaultPermissionConfig(), nil)
	return gateway
}

// faceCtx 给 ctx 盖上会话号（档位/员工身份的解析键）。
func faceCtx(sessionID string) context.Context {
	return context.WithValue(context.Background(), faceSessionKey{}, sessionID)
}

// faceDepsWithGate 是"装了闸门"的路径门依赖：判据直接接权限门本体。
func faceDepsWithGate(gateway *PermissionGate, scope *security.ProjectScope, sessionID string) Deps {
	return Deps{
		ProjectScope:   scope,
		SessionKey:     func(context.Context) string { return sessionID },
		FacePermission: gateway.FacePermission,
	}
}

// remoteDecisionContext 造"决策段选中 ssh 且已知远端项目根"的 ctx。
func remoteDecisionContext(ctx context.Context, remoteRoot string) context.Context {
	return WithTransportDecision(ctx, TransportDecision{
		Decided: true, Kind: TransportSSH, ProjectID: "ws-A", Feasible: true,
		Endpoint: SSHEndpoint{Host: "deploy.example", Port: 22, User: "deploy", RemoteRoot: remoteRoot},
		Reason:   ReasonProbeOK,
	})
}

// runPathGate 跑一次路径门，返回（handler 是否执行, err, 交接事实）。
func runPathGate(t *testing.T, gated frameworktools.MetaMiddleware, ctx context.Context, tool, args string) (bool, error, RemotePathHandoff, bool) {
	t.Helper()
	ran := false
	var handoff RemotePathHandoff
	var held bool
	handler := gated(tool, frameworktools.ToolMeta{}, frameworktools.HandlerFunc(
		func(ctx context.Context, _ string) (string, error) {
			ran = true
			handoff, held = RemotePathHandoffFromContext(ctx)
			return "ran", nil
		}))
	_, err := handler.Execute(ctx, args)
	return ran, err, handoff, held
}

// TestRemoteUserspaceGate 是本次工作的主判据：同一条越界路径，按 userspace 位给/不给
// 得到相反结论；而根内路径永远不需要 userspace。
func TestRemoteUserspaceGate(t *testing.T) {
	const sessionID = "sess-userspace"
	scope, _, outsideLocal := boundScope(t, sessionID)
	const remoteRoot = "/srv/app"
	inRoot := remoteRoot + "/src/main.go"

	t.Run("远端根内路径不需要 userspace（默认 manual 档也放行）", func(t *testing.T) {
		gateway := newFaceGate(t) // manual 档：有 ssh 通道位，没有 ssh.userspace
		gated := PathGate(faceDepsWithGate(gateway, scope, sessionID))
		ctx := remoteDecisionContext(faceCtx(sessionID), remoteRoot)

		ran, err, handoff, held := runPathGate(t, gated, ctx, ReadFileToolName, `{"path":`+quoteJSON(inRoot)+`}`)
		if err != nil {
			t.Fatalf("远端根内路径不该被拒：%v", err)
		}
		if !ran {
			t.Fatal("远端根内路径应放行到 handler")
		}
		if held || len(handoff.Candidates) != 0 {
			t.Fatalf("根内路径不该产生越界交接：%+v held=%v", handoff, held)
		}
	})

	t.Run("远端越界且授予 userspace → 放行并留痕", func(t *testing.T) {
		gateway := newFaceGate(t)
		gateway.SetFullAccessFor(sessionID, true) // full 档：含 ssh.userspace
		gated := PathGate(faceDepsWithGate(gateway, scope, sessionID))
		ctx := remoteDecisionContext(faceCtx(sessionID), remoteRoot)

		ran, err, handoff, held := runPathGate(t, gated, ctx, ReadFileToolName, `{"path":"/etc/nginx/nginx.conf"}`)
		if err != nil {
			t.Fatalf("授予 ssh.userspace 的越界不该被拒：%v", err)
		}
		if !ran {
			t.Fatal("授予 userspace 后应放行到 handler")
		}
		if !held || len(handoff.Candidates) != 1 || handoff.Candidates[0] != "/etc/nginx/nginx.conf" {
			t.Fatalf("越界事实应作为留痕写进 ctx：%+v held=%v", handoff, held)
		}
		if handoff.RemoteRoot != remoteRoot {
			t.Fatalf("留痕里的远端根 = %q，want %q（取根口径只此一处）", handoff.RemoteRoot, remoteRoot)
		}
	})

	t.Run("远端越界未授予 userspace → 拒（可结构化区分）", func(t *testing.T) {
		gateway := newFaceGate(t) // manual：没有 ssh.userspace
		gated := PathGate(faceDepsWithGate(gateway, scope, sessionID))
		ctx := remoteDecisionContext(faceCtx(sessionID), remoteRoot)

		ran, err, _, _ := runPathGate(t, gated, ctx, ReadFileToolName, `{"path":"/etc/nginx/nginx.conf"}`)
		if err == nil {
			t.Fatal("越界且没有 ssh.userspace 必须拒，不得静默放行到对端")
		}
		if ran {
			t.Fatal("被拒的调用不得进入 handler")
		}
		if code := FacePermissionErrorCode(err); code != CodePermSSHUserspaceRequired {
			t.Fatalf("错误码 = %q，want %q（err=%v）", code, CodePermSSHUserspaceRequired, err)
		}
		if !IsFacePermissionError(err, policy.GroupSSHUserspace) {
			t.Fatalf("拒绝应可结构化定位到组 %s：%v", policy.GroupSSHUserspace, err)
		}
		var face *FacePermissionError
		if !errors.As(err, &face) || len(face.Paths) != 1 || face.Paths[0] != "/etc/nginx/nginx.conf" {
			t.Fatalf("拒绝必须带上触发它的远端路径：%+v", face)
		}
		if IsTransportErrorCode(err) {
			t.Fatal("权限拒绝不得携带 SSH_* 传输码（两类失败必须结构性可分）")
		}
	})

	t.Run("远端根未知时按保守口径：本地根容不下的绝对路径也算越界", func(t *testing.T) {
		gateway := newFaceGate(t)
		gated := PathGate(faceDepsWithGate(gateway, scope, sessionID))
		ctx := remoteDecisionContext(faceCtx(sessionID), "") // 未配远端工作目录

		ran, err, _, _ := runPathGate(t, gated, ctx, ReadFileToolName, `{"path":`+quoteJSON(outsideLocal)+`}`)
		if code := FacePermissionErrorCode(err); code != CodePermSSHUserspaceRequired || ran {
			t.Fatalf("远端根未知 + 本地根外的绝对路径应过 userspace 闸门：code=%q ran=%v err=%v", code, ran, err)
		}
	})
}

// TestRemoteChannelBitGate：通道位（ssh）是独立的闸门——显式收回就拦，与路径无关。
func TestRemoteChannelBitGate(t *testing.T) {
	const sessionID = "sess-channel"
	scope, _, _ := boundScope(t, sessionID)

	// 员工显式格子：只读 + **明确收回** ssh 通道位（`{"ssh": 0}` 的语义）。
	revoked := WithEmployeeGrant(faceCtx(sessionID), "worker", "readonly", map[string]uint8{
		policy.GroupRO:           policy.BitRead,
		policy.GroupSSH:          0,
		policy.GroupSSHUserspace: 0,
	})
	gateway := newFaceGate(t)
	gated := PathGate(faceDepsWithGate(gateway, scope, sessionID))
	ctx := remoteDecisionContext(revoked, "/srv/app")

	ran, err, _, _ := runPathGate(t, gated, ctx, ReadFileToolName, `{"path":"/srv/app/src/main.go"}`)
	if code := FacePermissionErrorCode(err); code != CodePermSSHRequired || ran {
		t.Fatalf("收回 ssh 通道位后远端面必须拒：code=%q ran=%v err=%v", code, ran, err)
	}
	if !IsFacePermissionError(err, policy.GroupSSH) {
		t.Fatalf("拒绝应定位到组 %s：%v", policy.GroupSSH, err)
	}

	// 同一员工，把通道位给回来（其余不变）：放行——区别只有那一格。
	granted := WithEmployeeGrant(faceCtx(sessionID), "worker", "readonly", map[string]uint8{
		policy.GroupRO:  policy.BitRead,
		policy.GroupSSH: policy.BitExecute,
	})
	gated = PathGate(faceDepsWithGate(gateway, scope, sessionID))
	ran, err, _, _ = runPathGate(t, gated, remoteDecisionContext(granted, "/srv/app"), ReadFileToolName,
		`{"path":"/srv/app/src/main.go"}`)
	if err != nil || !ran {
		t.Fatalf("给了 ssh 通道位的员工应能远端读根内文件：err=%v ran=%v", err, ran)
	}
}

// TestLocalFaceUnaffectedByFaceGates：本地执行面逐字不变（这两道门只挂在远端面上）。
func TestLocalFaceUnaffectedByFaceGates(t *testing.T) {
	const sessionID = "sess-local"
	scope, localRoot, outside := boundScope(t, sessionID)
	gateway := newFaceGate(t) // manual：没有 userspace 位
	gated := PathGate(faceDepsWithGate(gateway, scope, sessionID))
	ctx := faceCtx(sessionID)

	ran, err, _, _ := runPathGate(t, gated, ctx, ReadFileToolName,
		`{"path":`+quoteJSON(localRoot)+`}`)
	if err != nil || !ran {
		t.Fatalf("本地面根内路径必须放行（与改动前一致）：err=%v ran=%v", err, ran)
	}

	ran, err, _, _ = runPathGate(t, gated, ctx, ReadFileToolName, `{"path":`+quoteJSON(outside)+`}`)
	if err == nil || !strings.Contains(err.Error(), "bound project") {
		t.Fatalf("本地面越界仍须按项目根拒（文案与改动前逐字一致）：%v", err)
	}
	if ran {
		t.Fatal("本地面被拒的调用不得进入 handler")
	}
}

// TestFaceGateConcurrentRolesDoNotPollute：两个角色并发判定互不污染。
//
// 一个员工**授予** userspace、另一个**没有**，同一个 ctx 键上并行跑（同一权限门
// 实例、同一会话）。结论必须各自稳定：允许的一直允许，拒的一直拒——判定只读 ctx 上
// 的身份，不写共享字段。
func TestFaceGateConcurrentRolesDoNotPollute(t *testing.T) {
	const sessionID = "sess-concurrent"
	scope, _, _ := boundScope(t, sessionID)
	gateway := newFaceGate(t)
	gated := PathGate(faceDepsWithGate(gateway, scope, sessionID))

	permitted := WithEmployeeGrant(faceCtx(sessionID), "deployer", "readwrite", map[string]uint8{
		policy.GroupRO:           policy.BitRead,
		policy.GroupRW:           policy.BitRead | policy.BitWrite,
		policy.GroupSSH:          policy.BitExecute,
		policy.GroupSSHUserspace: policy.BitRead | policy.BitWrite | policy.BitExecute,
	})
	denied := WithEmployeeGrant(faceCtx(sessionID), "reviewer", "readonly", map[string]uint8{
		policy.GroupRO:           policy.BitRead,
		policy.GroupRW:           0,
		policy.GroupSSH:          policy.BitExecute,
		policy.GroupSSHUserspace: 0,
	})

	const rounds = 40
	var wg sync.WaitGroup
	errorsSeen := make(chan string, rounds*2)
	allowedCtx := remoteDecisionContext(permitted, "/srv/app")
	deniedCtx := remoteDecisionContext(denied, "/srv/app")

	for i := 0; i < rounds; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			ran, err, _, _ := runPathGate(t, gated, allowedCtx, ReadFileToolName, `{"path":"/etc/passwd"}`)
			if err != nil || !ran {
				errorsSeen <- "授予 userspace 的角色被拒了：" + errString(err)
			}
		}()
		go func() {
			defer wg.Done()
			ran, err, _, _ := runPathGate(t, gated, deniedCtx, ReadFileToolName, `{"path":"/etc/passwd"}`)
			if ran || FacePermissionErrorCode(err) != CodePermSSHUserspaceRequired {
				errorsSeen <- "未授予 userspace 的角色被放行了：err=" + errString(err)
			}
		}()
	}
	wg.Wait()
	close(errorsSeen)
	for message := range errorsSeen {
		t.Fatal(message)
	}
}

func TestRemoteUserspaceAllowlistIsFailClosed(t *testing.T) {
	const sessionID = "sess-user-space-list"
	scope, _, _ := boundScope(t, sessionID)
	gateway := newFaceGate(t)
	gated := PathGate(faceDepsWithGate(gateway, scope, sessionID))
	ctx := WithEmployeeGrant(faceCtx(sessionID), "deployer", "readwrite", map[string]uint8{
		policy.GroupRO:           policy.BitRead,
		policy.GroupRW:           policy.BitRead | policy.BitWrite,
		policy.GroupSSH:          policy.BitExecute,
		policy.GroupSSHUserspace: policy.BitRead | policy.BitWrite | policy.BitExecute,
	})

	allowedCtx := remoteDecisionContext(WithRemoteUserSpaces(ctx, []string{"/home/seelex/.cache"}), "/srv/app")
	ran, err, _, _ := runPathGate(t, gated, allowedCtx, ReadFileToolName, `{"path":"/home/seelex/.cache/build.log"}`)
	if err != nil || !ran {
		t.Fatalf("允许目录内路径应放行：err=%v ran=%v", err, ran)
	}

	deniedCtx := remoteDecisionContext(WithRemoteUserSpaces(ctx, []string{"/home/seelex/.cache"}), "/srv/app")
	ran, err, _, _ = runPathGate(t, gated, deniedCtx, ReadFileToolName, `{"path":"/etc/passwd"}`)
	if ran || FacePermissionErrorCode(err) != CodePermSSHUserspacePathDenied {
		t.Fatalf("允许目录外必须拒：ran=%v err=%v", ran, err)
	}

	emptyCtx := remoteDecisionContext(WithRemoteUserSpaces(ctx, nil), "/srv/app")
	ran, err, _, _ = runPathGate(t, gated, emptyCtx, ReadFileToolName, `{"path":"/home/seelex/.cache/build.log"}`)
	if ran || FacePermissionErrorCode(err) != CodePermSSHUserspacePathDenied {
		t.Fatalf("空路径列表必须 fail-closed：ran=%v err=%v", ran, err)
	}
}

func errString(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

// TestFacePermissionDepsNilKeepsLegacyBehavior：没装闸门（未装配）时退回旧行为——
// 只留痕、不拒绝。这条钉住"装配漏了不会静默变成一个更严/更松的系统"。
func TestFacePermissionDepsNilKeepsLegacyBehavior(t *testing.T) {
	const sessionID = "sess-legacy"
	scope, _, _ := boundScope(t, sessionID)
	gated := PathGate(Deps{ProjectScope: scope, SessionKey: func(context.Context) string { return sessionID }})
	ctx := remoteDecisionContext(faceCtx(sessionID), "/srv/app")

	ran, err, handoff, held := runPathGate(t, gated, ctx, ReadFileToolName, `{"path":"/etc/passwd"}`)
	if err != nil || !ran {
		t.Fatalf("未装闸门时应保持旧行为（放行）：err=%v ran=%v", err, ran)
	}
	if !held || len(handoff.Candidates) != 1 {
		t.Fatalf("未装闸门时仍要留痕（交接事实）：%+v held=%v", handoff, held)
	}
}

// TestSSHGroupMatchIsTheRemoteFaceRoster：组表是"远端面工具清单"的唯一事实源。
//
// 若这两处漂移，registry 的消费者闸门（TransportConsumers）就会与真正读执行面的
// handler 对不上——表现是"选了 ssh 却跑本地"，而那种错没有任何报错面。
func TestSSHGroupMatchIsTheRemoteFaceRoster(t *testing.T) {
	roster := RemoteFaceToolNames()
	want := []string{ReadFileToolName, WriteFileToolName, EditFileToolName, BashToolName, bgManageToolName}
	if len(roster) != len(want) {
		t.Fatalf("远端面工具名单 = %v，want %v（派生自 policy 组表的 ssh 组 Match）", roster, want)
	}
	for index := range want {
		if roster[index] != want[index] {
			t.Fatalf("远端面工具名单 = %v，want %v（组表与 handler 必须同一份）", roster, want)
		}
	}
	// 名字路由不变：这五个工具仍归操作类型组（ro / rw），不归 ssh 组。
	for _, name := range want {
		group, routed := policy.RoutePermissionGroup(policy.DefaultPermissionGroupList(), name)
		if !routed || (group.Name != policy.GroupRO && group.Name != policy.GroupRW) {
			t.Fatalf("工具 %s 的路由组 = %q（routed=%v），want ro/rw（按面判定的组不得接管名字路由）",
				name, group.Name, routed)
		}
	}
	_ = dto.PermissionGroupNames()
}
