package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/internal/model"
)

// writeFixture 在临时目录里摆一份"用户手写、带注释"的账号档。
func writeFixture(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("写现场失败: %v", err)
	}
	return path
}

const commentedAccounts = `# 头部说明：这份文件是手写的。
defaults:
  provider: openai
  max_tokens: 16384

roles:
  # 主对话角色
  agent:
  - model: deepseek-flash
    base_url: https://api.deepseek.com
    api_key: sk-old-agent
    reasoning_effort: session
  subagent:
  - model: deepseek-flash
    api_key: sk-old-subagent
`

// TestAddAccountAppendsIntoExistingRoleBlock 是这条链路的主判据：新条目插进目标角色
// 块尾，其余字节一个不动（注释、缩进、其它角色都原样），加载器读回来的名字是
// agent-2、字段与输入一致。
func TestAddAccountAppendsIntoExistingRoleBlock(t *testing.T) {
	path := writeFixture(t, "accounts.yaml", commentedAccounts)

	name, err := AddAccount(path, NewAccountInput{
		Role:    "agent",
		Model:   "deepseek-flash",
		BaseURL: "https://api.deepseek.com",
		APIKey:  "sk-new-agent",
	})
	if err != nil {
		t.Fatalf("AddAccount: %v", err)
	}
	if name != "agent-2" {
		t.Fatalf("新账号名 = %q，期望 agent-2", name)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	want := strings.Replace(commentedAccounts,
		"    reasoning_effort: session\n",
		"    reasoning_effort: session\n"+
			"  - name: agent-2\n"+
			"    model: deepseek-flash\n"+
			"    base_url: https://api.deepseek.com\n"+
			"    api_key: sk-new-agent\n",
		1)
	if string(got) != want {
		t.Fatalf("落盘内容与期望不一致（只应多出条目四行）。\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	spec, err := findSpec(cfg, "agent-2")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if spec.APIKey != "sk-new-agent" || spec.BaseURL != "https://api.deepseek.com" || spec.Model != "deepseek-flash" {
		t.Fatalf("新账号字段不符: %#v", spec)
	}
	// 只该多一个账号：其它角色不受影响。
	if n := countRole(cfg.Specs, model.RoleSubAgent); n != 1 {
		t.Fatalf("subagent 账号数 = %d，期望 1", n)
	}
}

// TestAddAccountCreatesMissingRoleBlock 覆盖"这个角色还没有块"的路径：在 roles 块尾
// 建块，而不是插到别的角色下面。
func TestAddAccountCreatesMissingRoleBlock(t *testing.T) {
	path := writeFixture(t, "accounts.yaml", commentedAccounts)

	name, err := AddAccount(path, NewAccountInput{
		Role:            "teammate",
		Model:           "deepseek-flash",
		APIKey:          "sk-team",
		ReasoningEffort: "session",
	})
	if err != nil {
		t.Fatalf("AddAccount: %v", err)
	}
	if name != "teammate-1" {
		t.Fatalf("新账号名 = %q，期望 teammate-1", name)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if !strings.HasSuffix(string(got), "  teammate:\n  - name: teammate-1\n    model: deepseek-flash\n    api_key: sk-team\n    reasoning_effort: session\n") {
		t.Fatalf("未在 roles 块尾建出 teammate 块:\n%s", got)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := findSpec(cfg, "teammate-1"); err != nil {
		t.Fatalf("%v", err)
	}
}

// TestAddAccountCreatesFileWhenMissing 覆盖"账号档不存在"：按最小骨架新建，
// 新建文件的权限是 0600（里面有密钥）。
func TestAddAccountCreatesFileWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config", "accounts.yaml")

	name, err := AddAccount(path, NewAccountInput{
		Role:     "agent",
		Provider: "openai",
		Model:    "gpt-4o",
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "sk-fresh",
	})
	if err != nil {
		t.Fatalf("AddAccount: %v", err)
	}
	if name != "agent-1" {
		t.Fatalf("新账号名 = %q，期望 agent-1", name)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("新文件不存在: %v", err)
	}
	// Windows 没有 POSIX 权限位（Chmod 只映射只读位，Perm() 恒为 0666），权限断言只在
	// POSIX 上有意义：那里 0600 才是"只有自己能读密钥"。
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("新文件权限 = %o，期望 600（里面是密钥）", perm)
		}
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	spec, err := findSpec(cfg, "agent-1")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if spec.Provider != "openai" || spec.APIKey != "sk-fresh" {
		t.Fatalf("新账号字段不符: %#v", spec)
	}
}

// TestAddAccountKeepsCRLF 覆盖 Windows 手写文件：换行风格不因插入而被改写。
func TestAddAccountKeepsCRLF(t *testing.T) {
	body := strings.ReplaceAll(commentedAccounts, "\n", "\r\n")
	path := writeFixture(t, "accounts.yaml", body)

	if _, err := AddAccount(path, NewAccountInput{Role: "agent", Model: "m", APIKey: "sk-2"}); err != nil {
		t.Fatalf("AddAccount: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if strings.Contains(strings.ReplaceAll(string(got), "\r\n", ""), "\n") {
		t.Fatalf("插入引入了裸 LF（换行风格被改写）:\n%q", got)
	}
	if !strings.Contains(string(got), "    api_key: sk-2\r\n") {
		t.Fatalf("新条目未按 CRLF 写入:\n%q", got)
	}
}

// TestAddAccountRefusesWithoutTouchingFile 是"校验失败绝不落盘"的一组判据：
// 每种非法输入都要报错，且文件字节逐字不变（不能停在半改状态）。
func TestAddAccountRefusesWithoutTouchingFile(t *testing.T) {
	cases := []struct {
		name  string
		input NewAccountInput
		want  string
	}{
		{"缺少 api_key", NewAccountInput{Role: "agent", Model: "m"}, "api_key is required"},
		{"缺少 model", NewAccountInput{Role: "agent", APIKey: "sk"}, "model is required"},
		{"未知角色", NewAccountInput{Role: "butler", Model: "m", APIKey: "sk"}, "is not valid"},
		{"非法思考强度", NewAccountInput{Role: "agent", Model: "m", APIKey: "sk", ReasoningEffort: "extreme"}, "reasoning_effort"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			path := writeFixture(t, "accounts.yaml", commentedAccounts)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("读现场失败: %v", err)
			}
			if _, err := AddAccount(path, testCase.input); err == nil {
				t.Fatalf("期望报错，实际成功")
			} else if !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("错误文案 = %q，期望含 %q", err, testCase.want)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("读回失败: %v", err)
			}
			if string(before) != string(after) {
				t.Fatalf("校验失败却改动了文件")
			}
		})
	}
}

// TestAddAccountRefusesBrokenFile：解析不了的文件不覆盖（用户的现场可能有救）。
func TestAddAccountRefusesBrokenFile(t *testing.T) {
	const broken = "roles:\n  agent:\n  - model: [unclosed\n"
	path := writeFixture(t, "accounts.yaml", broken)
	_, err := AddAccount(path, NewAccountInput{Role: "agent", Model: "m", APIKey: "sk"})
	if err == nil {
		t.Fatalf("损坏的账号档上不该成功")
	}
	if !strings.Contains(err.Error(), "未改动文件") {
		t.Fatalf("错误文案应说明未改动文件: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != broken {
		t.Fatalf("损坏文件被改写了")
	}
}

// TestAddAccountRefusesFlowStyleRole：行内写法没法行级插入，必须明确拒绝而不是
// 插出第二个同名键（那会变成重复键报错，用户却看不出原因）。
func TestAddAccountRefusesFlowStyleRole(t *testing.T) {
	path := writeFixture(t, "accounts.yaml", "roles:\n  agent: [{model: m, api_key: sk}]\n")
	_, err := AddAccount(path, NewAccountInput{Role: "agent", Model: "m2", APIKey: "sk2"})
	if err == nil {
		t.Fatalf("flow 写法上不该成功")
	}
	if !strings.Contains(err.Error(), "行内写法") {
		t.Fatalf("错误文案应说明行内写法: %v", err)
	}
}

// TestAddAccountRepeatedlyKeepsOrdering：连加两条，名字按位置递增，条目顺序就是
// 追加顺序（名字由加载器推导，本函数不自己编，这条把两者钉在一起）。
func TestAddAccountRepeatedlyKeepsOrdering(t *testing.T) {
	path := writeFixture(t, "accounts.yaml", commentedAccounts)
	first, err := AddAccount(path, NewAccountInput{Role: "agent", Model: "m-a", APIKey: "sk-a"})
	if err != nil {
		t.Fatalf("第一次 AddAccount: %v", err)
	}
	second, err := AddAccount(path, NewAccountInput{Role: "agent", Model: "m-b", APIKey: "sk-b"})
	if err != nil {
		t.Fatalf("第二次 AddAccount: %v", err)
	}
	if first != "agent-2" || second != "agent-3" {
		t.Fatalf("名字 = %q, %q，期望 agent-2, agent-3", first, second)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	third, err := findSpec(cfg, "agent-3")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if third.Model != "m-b" {
		t.Fatalf("agent-3 的 model = %q，期望 m-b", third.Model)
	}
}

// TestAddAccountBlankFile 覆盖"文件在但内容为空"：与缺失同处置（骨架新建）。
func TestAddAccountBlankFile(t *testing.T) {
	path := writeFixture(t, "accounts.yaml", "  \n\n")
	name, err := AddAccount(path, NewAccountInput{Role: "subagent", Model: "m", APIKey: "sk"})
	if err != nil {
		t.Fatalf("AddAccount: %v", err)
	}
	if name != "subagent-1" {
		t.Fatalf("新账号名 = %q，期望 subagent-1", name)
	}
}

// TestAddAccountConcurrentAddsDoNotLoseEntries：并发添加不能丢条目。
//
// 这段是**读改写**，而账号名按位置推导：没有串行化时两个请求会读到同一份文件、算出
// 同一个 `agent-2`，后写的覆盖先写的（用户看到"加了两条只剩一条"）。判据：N 个并发
// 添加全部成功、名字互不相同、文件里正好多出 N 条。
func TestAddAccountConcurrentAddsDoNotLoseEntries(t *testing.T) {
	path := writeFixture(t, "accounts.yaml", commentedAccounts)

	const workers = 6
	names := make([]string, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			names[slot], errs[slot] = AddAccount(path, NewAccountInput{
				Role: "agent", Model: "m", APIKey: "sk-" + strconv.Itoa(slot),
			})
		}(index)
	}
	wg.Wait()

	seen := make(map[string]bool, workers)
	for index, err := range errs {
		if err != nil {
			t.Fatalf("第 %d 次并发添加失败: %v", index, err)
		}
		if seen[names[index]] {
			t.Fatalf("并发添加出现重名 %q（后写的覆盖了先写的）", names[index])
		}
		seen[names[index]] = true
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := countRole(cfg.Specs, model.RoleAgent); got != 1+workers {
		t.Fatalf("agent 账号数 = %d，期望 %d（原有 1 条 + 并发加的 %d 条）", got, 1+workers, workers)
	}
}

func TestUpdateAccountChangesOnlyTargetAndKeepsBlankAPIKey(t *testing.T) {
	path := writeFixture(t, "accounts.yaml", commentedAccounts)
	record, err := UpdateAccount(path, UpdateAccountInput{
		Name:            "agent-1",
		Provider:        "deepseek",
		Model:           "deepseek-reasoner",
		BaseURL:         "https://api.deepseek.com/v1",
		ReasoningEffort: "high",
	})
	if err != nil {
		t.Fatalf("UpdateAccount: %v", err)
	}
	if record.Name != "agent-1" || !record.APIKeySet {
		t.Fatalf("更新回执 = %+v，want agent-1 且密钥仍存在", record)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := findSpec(cfg, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Model != "deepseek-reasoner" || spec.Provider != "deepseek" ||
		spec.BaseURL != "https://api.deepseek.com/v1" || spec.ReasoningEffort != "high" {
		t.Fatalf("更新后的字段不符：%+v", spec)
	}
	if spec.APIKey != "sk-old-agent" {
		t.Fatalf("API key 留空应保留旧值，得到 %q", spec.APIKey)
	}
	if countRole(cfg.Specs, model.RoleSubAgent) != 1 {
		t.Fatal("更新 agent 不得影响 subagent")
	}
}

func TestDeleteAccountKeepsRemainingNamesStable(t *testing.T) {
	path := writeFixture(t, "accounts.yaml", `roles:
  agent:
  - model: first
    api_key: sk-first
  - model: second
    api_key: sk-second
`)
	record, err := DeleteAccount(path, "agent-1")
	if err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}
	if record.Name != "agent-1" {
		t.Fatalf("删除回执 = %+v", record)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Specs) != 1 {
		t.Fatalf("剩余账号 = %+v，want 1 条", cfg.Specs)
	}
	if _, err := findSpec(cfg, "agent-2"); err != nil {
		t.Fatalf("删除 agent-1 后，原 agent-2 必须保持名字稳定：%v", err)
	}
	if _, err := findSpec(cfg, "agent-1"); err == nil {
		t.Fatal("被删除的 agent-1 不得因位置重排而复活")
	}
}

func findSpec(cfg Config, name string) (model.AccountSpec, error) {
	for _, spec := range cfg.Specs {
		if spec.Name == name {
			return spec, nil
		}
	}
	return model.AccountSpec{}, fmt.Errorf("没有名为 %q 的账号（现有 %d 个）", name, len(cfg.Specs))
}
