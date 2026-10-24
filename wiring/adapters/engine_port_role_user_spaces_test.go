package adapters

import (
	"context"
	"reflect"
	"testing"

	toolreg "github.com/RedHuang-0622/seelex/backend/tool/registry"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

type userSpacesReactorEngine struct {
	fakeReactorEngine
	ctx context.Context
}

func (engine *userSpacesReactorEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error) {
	engine.ctx = ctx
	return engine.fakeReactorEngine.ChatStream(ctx, input, onChunk)
}

func TestEnginePortInjectsMainAgentSSHUserSpaces(t *testing.T) {
	for _, routed := range []bool{false, true} {
		name := "legacy"
		if routed {
			name = "session-routed"
		}
		t.Run(name, func(t *testing.T) {
			const sessionID = "sess-user-space"
			engine := &userSpacesReactorEngine{fakeReactorEngine: fakeReactorEngine{sessionID: sessionID}}
			port := NewEnginePort(nil, func(string) ReactorEngine { return engine }, nil)
			port.engines[sessionID] = engine
			port.SetRoleUserSpacesProvider(func(gotSession, role string) []string {
				if gotSession != sessionID || role != string(dto.RoleAgent) {
					t.Fatalf("provider = %q %q", gotSession, role)
				}
				return []string{"/srv/shared"}
			})
			var err error
			if routed {
				_, err = port.ChatStreamFor(sessionID, context.Background(), "hello", nil)
			} else {
				port.engine = engine
				port.sessionID = sessionID
				port.engines[sessionID] = engine
				_, err = port.ChatStream(context.Background(), "hello", nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			paths, present := toolreg.RemoteUserSpacesFromContext(engine.ctx)
			if !present || !reflect.DeepEqual(paths, []string{"/srv/shared"}) {
				t.Fatalf("paths=%v present=%v，主代理必须拿到角色路径范围", paths, present)
			}
		})
	}
}
