package seelebridge

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/types"
)

// TestCustomRoleLiveProbe 是 provider role 能力实验（默认跳过）：
// 在真实请求历史中放入一个非 system/user/assistant/tool 的角色名，观察
// Seele 是否原样发送、provider 是否接受。请求日志只记录 role + 内容 hash，
// 不落正文。
//
// 运行：
//
//	$env:SEELEX_LIVE_SMOKE='1'
//	go test ./seelebridge -run TestCustomRoleLiveProbe -v -count=1 -timeout 5m
//
// 可选：SEELEX_CUSTOM_ROLE（默认 tl）、SEELEX_ACCOUNTS_PATH（默认 ../config/accounts.yaml）。
func TestCustomRoleLiveProbe(t *testing.T) {
	if os.Getenv("SEELEX_LIVE_SMOKE") == "" {
		t.Skip("set SEELEX_LIVE_SMOKE=1 to run the real-API custom role probe")
	}
	role := strings.TrimSpace(os.Getenv("SEELEX_CUSTOM_ROLE"))
	if role == "" {
		role = "tl"
	}
	switch role {
	case "system", "user", "assistant", "tool":
		t.Fatalf("SEELEX_CUSTOM_ROLE=%q is a provider-standard role; set a non-standard role to run the probe", role)
	}
	accountsPath := os.Getenv("SEELEX_ACCOUNTS_PATH")
	if accountsPath == "" {
		accountsPath = filepath.Join("..", "config", "accounts.yaml")
	}
	logPath := filepath.Join(t.TempDir(), "custom-role-requests.jsonl")
	t.Setenv(requestLogEnv, logPath)

	runtime, err := NewRuntime(RuntimeConfig{
		AccountsPath:    accountsPath,
		ToolCallTimeout: 2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer runtime.Shutdown()
	sess, err := runtime.NewMainSessionWithID("custom-role-probe", nil)
	if err != nil {
		t.Fatalf("NewMainSessionWithID: %v", err)
	}
	content := "这是角色 " + role + " 的历史发言。"
	sess.AppendHistory(types.Message{Role: role, Content: &content})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	answer, chatErr := sess.Chat(ctx, "请只回复 OK，不要调用任何工具。")

	roles, readErr := customRoleProbeLoggedRoles(logPath)
	if readErr != nil {
		t.Fatalf("read request log: %v", readErr)
	}
	if len(roles) == 0 {
		t.Fatalf("request log did not capture the provider request (chatErr=%v)", chatErr)
	}
	found := false
	for _, candidate := range roles[0] {
		if candidate == role {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("request did not contain custom role %q; first request roles=%v", role, roles[0])
	}
	if chatErr != nil {
		if strings.Contains(strings.ToLower(chatErr.Error()), "unknown variant") ||
			strings.Contains(strings.ToLower(chatErr.Error()), "expected one of") {
			t.Logf("provider rejected custom role %q as expected: %v", role, chatErr)
			return
		}
		t.Fatalf("provider rejected custom role %q for an unexpected reason: %v", role, chatErr)
	}
	t.Logf("provider accepted custom role %q; answer=%q（仍保持逻辑角色只走 role_name metadata）", role, strings.TrimSpace(answer))
}

type customRoleProbeRequest struct {
	Messages []struct {
		Role string `json:"role"`
	} `json:"messages"`
}

func customRoleProbeLoggedRoles(path string) ([][]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var out [][]string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var entry customRoleProbeRequest
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			return nil, err
		}
		roles := make([]string, 0, len(entry.Messages))
		for _, message := range entry.Messages {
			roles = append(roles, message.Role)
		}
		out = append(out, roles)
	}
	return out, scanner.Err()
}
