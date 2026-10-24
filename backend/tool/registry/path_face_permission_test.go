package registry

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
	"github.com/RedHuang-0622/seelex/backend/limit/gate"
	"github.com/RedHuang-0622/seelex/backend/limit/policy"
)

type faceSessionKey struct{}

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

func faceCtx(sessionID string) context.Context {
	return context.WithValue(context.Background(), faceSessionKey{}, sessionID)
}

func faceDepsWithGate(gateway *PermissionGate, scope *security.ProjectScope, sessionID string) Deps {
	return Deps{
		ProjectScope:   scope,
		SessionKey:     func(context.Context) string { return sessionID },
		FacePermission: gateway.FacePermission,
	}
}

func remoteDecisionContext(ctx context.Context, remoteRoot string) context.Context {
	return WithTransportDecision(ctx, TransportDecision{
		Decided: true, Kind: TransportSSH, ProjectID: "ws-A", Feasible: true,
		Endpoint: SSHEndpoint{Host: "deploy.example", Port: 22, User: "deploy", RemoteRoot: remoteRoot},
		Reason:   ReasonProbeOK,
	})
}

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

func grantedSSHContext(sessionID string, paths []string) context.Context {
	ctx := WithEmployeeGrant(faceCtx(sessionID), "worker", "readonly", map[string]uint8{
		policy.GroupRO:  policy.BitRead,
		policy.GroupSSH: policy.BitExecute,
	})
	return WithRemoteUserSpaces(ctx, paths)
}

func TestSSHCapabilityCoversRemoteRootAndAllowlist(t *testing.T) {
	const sessionID = "sess-ssh-single-capability"
	scope, _, _ := boundScope(t, sessionID)
	gateway := newFaceGate(t)
	gated := PathGate(faceDepsWithGate(gateway, scope, sessionID))

	t.Run("root path", func(t *testing.T) {
		ctx := remoteDecisionContext(grantedSSHContext(sessionID, []string{"/home/seelex/.cache"}), "/srv/app")
		ran, err, handoff, held := runPathGate(t, gated, ctx, ReadFileToolName, `{"path":"/srv/app/src/main.go"}`)
		if err != nil || !ran || held || len(handoff.Candidates) != 0 {
			t.Fatalf("root path err=%v ran=%v handoff=%+v held=%v", err, ran, handoff, held)
		}
	})

	t.Run("allowlisted path", func(t *testing.T) {
		ctx := remoteDecisionContext(grantedSSHContext(sessionID, []string{"/home/seelex/.cache"}), "/srv/app")
		ran, err, handoff, held := runPathGate(t, gated, ctx, ReadFileToolName, `{"path":"/home/seelex/.cache/build.log"}`)
		if err != nil || !ran || !held || len(handoff.Candidates) != 1 {
			t.Fatalf("allowlisted path err=%v ran=%v handoff=%+v held=%v", err, ran, handoff, held)
		}
	})

	t.Run("path outside allowlist", func(t *testing.T) {
		ctx := remoteDecisionContext(grantedSSHContext(sessionID, []string{"/home/seelex/.cache"}), "/srv/app")
		ran, err, _, _ := runPathGate(t, gated, ctx, ReadFileToolName, `{"path":"/etc/passwd"}`)
		if ran || FacePermissionErrorCode(err) != CodePermSSHPathDenied {
			t.Fatalf("outside allowlist ran=%v err=%v", ran, err)
		}
		var face *FacePermissionError
		if !errors.As(err, &face) || face.Group != policy.GroupSSH || len(face.Paths) != 1 {
			t.Fatalf("face error = %+v", face)
		}
	})

	t.Run("installed empty allowlist fails closed", func(t *testing.T) {
		ctx := remoteDecisionContext(grantedSSHContext(sessionID, nil), "/srv/app")
		ran, err, _, _ := runPathGate(t, gated, ctx, ReadFileToolName, `{"path":"/home/seelex/.cache/build.log"}`)
		if ran || FacePermissionErrorCode(err) != CodePermSSHPathDenied {
			t.Fatalf("empty allowlist ran=%v err=%v", ran, err)
		}
	})
}

func TestSSHRevocationBlocksRemoteFace(t *testing.T) {
	const sessionID = "sess-channel"
	scope, _, _ := boundScope(t, sessionID)
	gateway := newFaceGate(t)
	gated := PathGate(faceDepsWithGate(gateway, scope, sessionID))
	revoked := WithEmployeeGrant(faceCtx(sessionID), "worker", "readonly", map[string]uint8{
		policy.GroupRO:  policy.BitRead,
		policy.GroupSSH: 0,
	})
	ran, err, _, _ := runPathGate(t, gated, remoteDecisionContext(revoked, "/srv/app"),
		ReadFileToolName, `{"path":"/srv/app/src/main.go"}`)
	if ran || FacePermissionErrorCode(err) != CodePermSSHRequired || !IsFacePermissionError(err, policy.GroupSSH) {
		t.Fatalf("revoked ssh ran=%v err=%v", ran, err)
	}
}

func TestLocalFaceIsStillBoundToProjectRoot(t *testing.T) {
	const sessionID = "sess-local"
	scope, localRoot, outside := boundScope(t, sessionID)
	gated := PathGate(faceDepsWithGate(newFaceGate(t), scope, sessionID))
	ctx := faceCtx(sessionID)
	ran, err, _, _ := runPathGate(t, gated, ctx, ReadFileToolName, `{"path":`+quoteJSON(localRoot)+`}`)
	if err != nil || !ran {
		t.Fatalf("local root err=%v ran=%v", err, ran)
	}
	ran, err, _, _ = runPathGate(t, gated, ctx, ReadFileToolName, `{"path":`+quoteJSON(outside)+`}`)
	if ran || err == nil || !strings.Contains(err.Error(), "bound project") {
		t.Fatalf("local outside ran=%v err=%v", ran, err)
	}
}

func TestConcurrentSSHRolesDoNotPollute(t *testing.T) {
	const sessionID = "sess-concurrent"
	scope, _, _ := boundScope(t, sessionID)
	gated := PathGate(faceDepsWithGate(newFaceGate(t), scope, sessionID))
	allowed := remoteDecisionContext(WithRemoteUserSpaces(WithEmployeeGrant(
		faceCtx(sessionID), "allowed", "readonly", map[string]uint8{
			policy.GroupRO: policy.BitRead, policy.GroupSSH: policy.BitExecute,
		}), []string{"/etc"}), "/srv/app")
	denied := remoteDecisionContext(WithEmployeeGrant(
		faceCtx(sessionID), "denied", "readonly", map[string]uint8{
			policy.GroupRO: policy.BitRead, policy.GroupSSH: 0,
		}), "/srv/app")

	var wg sync.WaitGroup
	errorsSeen := make(chan string, 64)
	for i := 0; i < 32; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			ran, err, _, _ := runPathGate(t, gated, allowed, ReadFileToolName, `{"path":"/etc/passwd"}`)
			if err != nil || !ran {
				errorsSeen <- "allowed: " + errString(err)
			}
		}()
		go func() {
			defer wg.Done()
			ran, err, _, _ := runPathGate(t, gated, denied, ReadFileToolName, `{"path":"/etc/passwd"}`)
			if ran || FacePermissionErrorCode(err) != CodePermSSHRequired {
				errorsSeen <- "denied: " + errString(err)
			}
		}()
	}
	wg.Wait()
	close(errorsSeen)
	for message := range errorsSeen {
		t.Fatal(message)
	}
}

func TestFacePermissionDepsNilKeepsLegacyBehavior(t *testing.T) {
	const sessionID = "sess-legacy"
	scope, _, _ := boundScope(t, sessionID)
	gated := PathGate(Deps{ProjectScope: scope, SessionKey: func(context.Context) string { return sessionID }})
	ctx := remoteDecisionContext(faceCtx(sessionID), "/srv/app")
	ran, err, _, _ := runPathGate(t, gated, ctx, ReadFileToolName, `{"path":"/etc/passwd"}`)
	if err != nil || !ran {
		t.Fatalf("nil gate err=%v ran=%v", err, ran)
	}
}

func errString(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}
