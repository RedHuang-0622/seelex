// accounts_write.go — 「界面手动添加一个账号」的落盘路径。
//
// 为什么不是把 YAML 反序列化再序列化：accounts.yaml 是**用户手写并带注释**的配置
// （一份 200 行的说明文字 + 四条账号）。yaml.v3 的 Node 往返会保留注释，但会把
// 每个序列条目按自己的缩进规则重排（`  - model:` → `    - model:`），整份文件
// 因此被改写——对这种文件做"加一条"却产生全文件 diff 是不可接受的（也很难让人
// 复核到底改了什么）。所以这里做**行级插入**：只往角色块尾追加一段条目文本，其余
// 字节一个不动。
//
// 手写文本的风险用两道闸抵掉，绝不靠"看起来对"：
//
//  1. 插入点按结构定位（`roles:` → `  <role>:` → 该块最后一条内容行），不是字符串
//     查找；role 块写成行内 flow 风格这类"没法安全插"的形状直接报错不写；
//  2. 插入后的**整份字节**再走一遍真实加载器（parseAccounts，与启动装载同一条
//     代码），要求"该角色账号数 +1"且"新条目的字段与输入逐字相符"，任何一项不符
//     即放弃写入——配置不会停在半改状态。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/model"
)

// NewAccountInput 是角色卡下「添加账户」的输入（非敏感字段 + 凭据）。
//
// 字段口径与 accounts.yaml 的账号条目一致：能留空的都留空即继承 `defaults`，
// 与手写一条账号的效果完全相同（同一份加载器解析）。
type NewAccountInput struct {
	// Role 是账号角色（agent / subagent / goalplan / teammate）；决定它落进哪个
	// 角色块，也决定生成的名字（`<role>-<序号>`）。
	Role            string
	Provider        string
	Model           string
	BaseURL         string
	APIKey          string
	ReasoningEffort string
}

// AccountRecord 是账号管理界面可读的非秘密字段（APIKeySet 只报告是否已有密钥）。
type AccountRecord struct {
	Name            string
	Role            string
	Provider        string
	Model           string
	BaseURL         string
	ReasoningEffort string
	APIKeySet       bool
}

// UpdateAccountInput 是账号编辑的输入。Name 与 Role 是身份，不允许借编辑改名或搬家；
// APIKey 留空表示保留原密钥，避免界面为了改模型而要求重新输入凭据。
type UpdateAccountInput struct {
	Name            string
	Provider        string
	Model           string
	BaseURL         string
	APIKey          string
	ReasoningEffort string
}

// AccountRoles 返回可手动添加的角色名（界面下拉的数据源；词表本体在 contract/dto）。
func AccountRoles() []string {
	return dto.AccountRoleVocabulary()
}

// normalizeAccountRole 校验收敛角色名；未知返回错误而不是猜一个默认值。
func normalizeAccountRole(role string) (model.AccountRole, error) {
	trimmed := strings.ToLower(strings.TrimSpace(role))
	for _, candidate := range AccountRoles() {
		if trimmed == candidate {
			return model.AccountRole(candidate), nil
		}
	}
	return "", fmt.Errorf("seelex: account role %q is not valid, want one of %v", role, AccountRoles())
}

// accountWriteMu 把账号档的"读 → 改 → 复核 → 写"整段串行化。
//
// 为什么要它：这段是**读改写**，而账号名是"位置推导"的。两个界面请求并发进来时，
// 两边都会读到同一份文件、都算出 `agent-2`，后写的那次把先写的整条覆盖掉——用户看到
// "加了两条只剩一条"。串行化之后每次都能读到上一次的结果，序号自然递增。
//
// 边界：只串行化**本进程内**的写入。跨进程（同一份账号档被另一个实例改）不在本层
// 处理——那种情况靠插入后的整份复核把"读到的不是我以为的那份"暴露成错误，而不是
// 静默写坏。
var accountWriteMu sync.Mutex

// AddAccount 把一个新账号追加进账号档并返回它**生效的名字**（`<role>-<序号>`，
// 序号由加载器按条目位置推导，本函数不自己编）。
//
// 纪律：先校验后落盘；落盘前用真实加载器复核；写盘走同目录临时文件 → 原子替换。
// 文件不存在时按最小骨架新建（`defaults` + 该角色一条）——这与"配置缺失时加载器
// 用内置兜底账号"并不冲突：新建之后这份文件就是账号档本身。
func AddAccount(path string, input NewAccountInput) (string, error) {
	accountWriteMu.Lock()
	defer accountWriteMu.Unlock()

	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("seelex: accounts path is empty")
	}
	role, err := normalizeAccountRole(input.Role)
	if err != nil {
		return "", err
	}
	modelName := strings.TrimSpace(input.Model)
	if modelName == "" {
		return "", fmt.Errorf("seelex: account model is required")
	}
	apiKey := strings.TrimSpace(input.APIKey)
	if apiKey == "" {
		return "", fmt.Errorf("seelex: account api_key is required")
	}
	effort := strings.TrimSpace(input.ReasoningEffort)
	if effort != "" && !model.ValidReasoningEffort(effort) {
		return "", fmt.Errorf("seelex: reasoning_effort %q is not valid, want one of %v",
			input.ReasoningEffort, model.ReasoningEffortVocabulary())
	}

	raw, mode, exists, err := readAccountsFile(path)
	if err != nil {
		return "", err
	}
	// 空文件与"文件不存在"同处置：都按最小骨架新建（空文件本就过不了加载器，
	// 它此刻在启动期只会换来一条兜底警告）。
	blank := !exists || len(strings.TrimSpace(string(raw))) == 0

	// before 是"加之前这份文件读出来长什么样"：既是插入点的结构依据（该角色目前
	// 有几个账号 → 新名字），也是复核的基准（加完必须正好多一个）。
	var before Config
	if !blank {
		before, err = parseAccounts(raw)
		if err != nil {
			return "", fmt.Errorf("seelex: %s 当前内容无法解析（未改动文件）: %w", path, err)
		}
	}

	entry := pendingAccount{
		role:     role,
		name:     nextAccountName(before.Specs, role),
		model:    modelName,
		baseURL:  strings.TrimSpace(input.BaseURL),
		apiKey:   apiKey,
		effort:   effort,
		provider: strings.TrimSpace(input.Provider),
	}
	updated, err := insertAccountEntry(raw, blank, entry)
	if err != nil {
		return "", err
	}

	name, err := verifyInsertedAccount(updated, before, role, entry)
	if err != nil {
		return "", err
	}

	if err := writeFileAtomic(path, updated, mode); err != nil {
		return "", fmt.Errorf("seelex: write %s: %w", path, err)
	}
	return name, nil
}

// ReadAccount 读取一个账号的非秘密字段。
func ReadAccount(path, name string) (AccountRecord, error) {
	if strings.TrimSpace(path) == "" {
		return AccountRecord{}, fmt.Errorf("seelex: accounts path is empty")
	}
	loaded, err := Load(path)
	if err != nil {
		return AccountRecord{}, err
	}
	spec := accountSpecByName(loaded.Specs, strings.TrimSpace(name))
	if spec == nil {
		return AccountRecord{}, fmt.Errorf("seelex: unknown account %q", name)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return AccountRecord{}, err
	}
	var source simplifiedConfig
	if err := yaml.Unmarshal(raw, &source); err != nil {
		return AccountRecord{}, fmt.Errorf("seelex: read account %q: %w", name, err)
	}
	roleEntries := map[model.AccountRole][]simplifiedAccount{
		model.RoleAgent:    source.Roles.Agent,
		model.RoleSubAgent: source.Roles.SubAgent,
		model.RoleGoalPlan: source.Roles.GoalPlan,
		model.RoleTeammate: source.Roles.Teammate,
	}[spec.Role]
	ordinal := 0
	for _, candidate := range loaded.Specs {
		if candidate.Role != spec.Role {
			continue
		}
		ordinal++
		if candidate.Name == spec.Name {
			break
		}
	}
	if ordinal <= 0 || ordinal > len(roleEntries) {
		return AccountRecord{}, fmt.Errorf("seelex: account %q has no matching YAML entry", name)
	}
	entry := roleEntries[ordinal-1]
	return AccountRecord{
		Name:            spec.Name,
		Role:            string(spec.Role),
		Provider:        strings.TrimSpace(entry.Provider),
		Model:           strings.TrimSpace(entry.Model),
		BaseURL:         strings.TrimSpace(entry.BaseURL),
		ReasoningEffort: strings.TrimSpace(entry.ReasoningEffort),
		APIKeySet:       strings.TrimSpace(entry.APIKey) != "",
	}, nil
}

// UpdateAccount 原位更新一条账号，保留条目外的注释与其它账号字节。
func UpdateAccount(path string, input UpdateAccountInput) (AccountRecord, error) {
	accountWriteMu.Lock()
	defer accountWriteMu.Unlock()

	name := strings.TrimSpace(input.Name)
	if name == "" {
		return AccountRecord{}, fmt.Errorf("seelex: account name is required")
	}
	modelName := strings.TrimSpace(input.Model)
	if modelName == "" {
		return AccountRecord{}, fmt.Errorf("seelex: account model is required")
	}
	effort := strings.TrimSpace(input.ReasoningEffort)
	if effort != "" && !model.ValidReasoningEffort(effort) {
		return AccountRecord{}, fmt.Errorf("seelex: reasoning_effort %q is not valid, want one of %v",
			input.ReasoningEffort, model.ReasoningEffortVocabulary())
	}

	raw, mode, exists, err := readAccountsFile(path)
	if err != nil {
		return AccountRecord{}, err
	}
	if !exists || len(strings.TrimSpace(string(raw))) == 0 {
		return AccountRecord{}, fmt.Errorf("seelex: accounts path %s is empty", path)
	}
	before, err := parseAccounts(raw)
	if err != nil {
		return AccountRecord{}, fmt.Errorf("seelex: %s 当前内容无法解析（未改动文件）: %w", path, err)
	}
	old := accountSpecByName(before.Specs, name)
	if old == nil {
		return AccountRecord{}, fmt.Errorf("seelex: unknown account %q", name)
	}
	apiKey := strings.TrimSpace(input.APIKey)
	if apiKey == "" {
		apiKey = old.APIKey
	}
	entry := pendingAccount{
		role:     old.Role,
		name:     old.Name,
		model:    modelName,
		baseURL:  strings.TrimSpace(input.BaseURL),
		apiKey:   apiKey,
		effort:   effort,
		provider: strings.TrimSpace(input.Provider),
	}

	lines, eol, trailing := splitAccountLines(raw)
	lines, err = freezeRoleAccountNames(lines, old.Role, before.Specs)
	if err != nil {
		return AccountRecord{}, err
	}
	staged := joinAccountLines(lines, eol, trailing)
	stagedCfg, err := parseAccounts(staged)
	if err != nil || !sameRoleAccountNames(before.Specs, stagedCfg.Specs, old.Role) {
		return AccountRecord{}, fmt.Errorf("seelex: 冻结账号名失败（已放弃写入）")
	}
	lines, eol, trailing = splitAccountLines(staged)
	start, end, itemIndent, err := findAccountItem(lines, old.Role, name)
	if err != nil {
		return AccountRecord{}, err
	}
	contentEnd := trimItemTail(lines, start, end)
	replacement := entry.entryLines(itemIndent)
	updated := replaceLineRange(lines, start, contentEnd, replacement)
	payload := joinAccountLines(updated, eol, trailing)
	after, err := parseAccounts(payload)
	if err != nil {
		return AccountRecord{}, fmt.Errorf("seelex: 更新账号后账号档无法解析（已放弃写入）: %w", err)
	}
	got := accountSpecByName(after.Specs, name)
	if got == nil || got.Role != old.Role || got.Model != entry.model || got.BaseURL != entry.baseURL ||
		got.APIKey != entry.apiKey {
		return AccountRecord{}, fmt.Errorf("seelex: 更新账号 %q 的校验失败（已放弃写入）", name)
	}
	if countRole(after.Specs, old.Role) != countRole(before.Specs, old.Role) {
		return AccountRecord{}, fmt.Errorf("seelex: 更新账号 %q 改变了角色条目数（已放弃写入）", name)
	}
	if err := writeFileAtomic(path, payload, mode); err != nil {
		return AccountRecord{}, fmt.Errorf("seelex: write %s: %w", path, err)
	}
	return accountRecordFromSpec(*got), nil
}

// DeleteAccount 删除一条账号；删除前先把该角色现有名字冻结，避免后续条目因位置
// 重排而被“改名复活”。
func DeleteAccount(path, name string) (AccountRecord, error) {
	accountWriteMu.Lock()
	defer accountWriteMu.Unlock()

	name = strings.TrimSpace(name)
	if name == "" {
		return AccountRecord{}, fmt.Errorf("seelex: account name is required")
	}
	raw, mode, exists, err := readAccountsFile(path)
	if err != nil {
		return AccountRecord{}, err
	}
	if !exists || len(strings.TrimSpace(string(raw))) == 0 {
		return AccountRecord{}, fmt.Errorf("seelex: accounts path %s is empty", path)
	}
	before, err := parseAccounts(raw)
	if err != nil {
		return AccountRecord{}, fmt.Errorf("seelex: %s 当前内容无法解析（未改动文件）: %w", path, err)
	}
	old := accountSpecByName(before.Specs, name)
	if old == nil {
		return AccountRecord{}, fmt.Errorf("seelex: unknown account %q", name)
	}
	if len(before.Specs) == 1 {
		return AccountRecord{}, fmt.Errorf("seelex: 不能删除最后一条账号（配置会没有任何可用凭据）")
	}
	lines, eol, trailing := splitAccountLines(raw)
	lines, err = freezeRoleAccountNames(lines, old.Role, before.Specs)
	if err != nil {
		return AccountRecord{}, err
	}
	staged := joinAccountLines(lines, eol, trailing)
	stagedCfg, err := parseAccounts(staged)
	if err != nil || !sameRoleAccountNames(before.Specs, stagedCfg.Specs, old.Role) {
		return AccountRecord{}, fmt.Errorf("seelex: 冻结账号名失败（已放弃写入）")
	}
	lines, eol, trailing = splitAccountLines(staged)
	start, end, _, err := findAccountItem(lines, old.Role, name)
	if err != nil {
		return AccountRecord{}, err
	}
	updated := removeLineRange(lines, start, trimItemTail(lines, start, end))
	payload := joinAccountLines(updated, eol, trailing)
	after, err := parseAccounts(payload)
	if err != nil {
		return AccountRecord{}, fmt.Errorf("seelex: 删除账号后账号档无法解析（已放弃写入）: %w", err)
	}
	if accountSpecByName(after.Specs, name) != nil || len(after.Specs) != len(before.Specs)-1 {
		return AccountRecord{}, fmt.Errorf("seelex: 删除账号 %q 的校验失败（已放弃写入）", name)
	}
	if countRole(after.Specs, old.Role) != countRole(before.Specs, old.Role)-1 {
		return AccountRecord{}, fmt.Errorf("seelex: 删除账号 %q 后角色条目数不符（已放弃写入）", name)
	}
	if err := writeFileAtomic(path, payload, mode); err != nil {
		return AccountRecord{}, fmt.Errorf("seelex: write %s: %w", path, err)
	}
	return accountRecordFromSpec(*old), nil
}

// pendingAccount 是一条待写入的账号条目（AddAccount 校验之后的规范形态）。
type pendingAccount struct {
	role     model.AccountRole
	name     string
	model    string
	baseURL  string
	apiKey   string
	effort   string
	provider string
}

// entryLines 按 accounts.yaml 既有条目的字段顺序渲染这条账号（新行插在角色块尾，
// 所以它必须与手写条目的形状一致：model 在前，base_url/api_key 跟随，
// reasoning_effort 与 provider 只在有值时出现）。
func (entry pendingAccount) entryLines(itemIndent int) []string {
	pad := strings.Repeat(" ", itemIndent)
	field := strings.Repeat(" ", itemIndent+2)
	lines := []string{pad + "- name: " + yamlScalar(entry.name)}
	lines = append(lines, field+"model: "+yamlScalar(entry.model))
	if entry.baseURL != "" {
		lines = append(lines, field+"base_url: "+yamlScalar(entry.baseURL))
	}
	lines = append(lines, field+"api_key: "+yamlScalar(entry.apiKey))
	if entry.effort != "" {
		lines = append(lines, field+"reasoning_effort: "+yamlScalar(entry.effort))
	}
	if entry.provider != "" {
		lines = append(lines, field+"provider: "+yamlScalar(entry.provider))
	}
	return lines
}

// yamlScalar 渲染一个标量值：能裸写就裸写（与手写条目同形），不能就双引号转义。
//
// 判据保守：含 `#`、`: `、控制字符，或以 YAML 指示符开头/结尾带空格的值一律加引号。
// 真正的正确性由插入后的整份复核（parseAccounts 读回来的字段逐字比对）兜底——这里
// 只是让常见值（模型名、URL、sk- 密钥）保持可读。
func yamlScalar(value string) string {
	if value == "" {
		return `""`
	}
	if strings.TrimSpace(value) != value {
		return strconv.Quote(value)
	}
	if strings.ContainsAny(value, "#\"'\t\n\r") || strings.Contains(value, ": ") {
		return strconv.Quote(value)
	}
	switch value[0] {
	case '-', '?', ':', ',', '[', ']', '{', '}', '&', '*', '!', '|', '>', '@', '`', '%':
		return strconv.Quote(value)
	}
	if strings.HasSuffix(value, ":") {
		return strconv.Quote(value)
	}
	return value
}

// readAccountsFile 读账号档；报告文件是否存在、原始权限位（新建时 0600）。
func readAccountsFile(path string) (raw []byte, mode os.FileMode, exists bool, err error) {
	info, statErr := os.Stat(path)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return nil, 0o600, false, nil
		}
		return nil, 0, false, fmt.Errorf("seelex: stat %s: %w", path, statErr)
	}
	if info.IsDir() {
		return nil, 0, false, fmt.Errorf("seelex: %s is a directory, not an accounts file", path)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return nil, 0, false, fmt.Errorf("seelex: read %s: %w", path, readErr)
	}
	return data, info.Mode().Perm(), true, nil
}

// insertAccountEntry 把一条账号条目插进角色块尾，返回新的整份字节。
//
// blank = 账号档不存在或为空：直接给一份最小骨架（roles 下只有这一个角色块）。
func insertAccountEntry(raw []byte, blank bool, entry pendingAccount) ([]byte, error) {
	roleKey := string(entry.role)

	if blank {
		skeleton := []string{
			"# 账号档（由角色卡下的「添加账户」创建；字段口径同 accounts.example.yaml）。",
			"roles:",
			"  " + roleKey + ":",
		}
		skeleton = append(skeleton, entry.entryLines(2)...)
		return []byte(strings.Join(skeleton, "\n") + "\n"), nil
	}

	text := string(raw)
	eol := "\n"
	if strings.Contains(text, "\r\n") {
		eol = "\r\n"
	}
	lines := strings.Split(text, "\n")
	// lines 的最后一项是 EOF 之后的那段（正常文件是空串）；插入只在这之前的真实行里做。
	trailingNewline := strings.HasSuffix(text, "\n")
	if trailingNewline {
		lines = lines[:len(lines)-1]
	}
	stripped := make([]string, len(lines))
	for index, line := range lines {
		stripped[index] = strings.TrimSuffix(line, "\r")
	}

	rolesIndex := -1
	for index, line := range stripped {
		if line == "roles:" {
			rolesIndex = index
			break
		}
	}

	roleIndent, roleIndex := -1, -1
	if rolesIndex >= 0 {
		for index := rolesIndex + 1; index < len(stripped); index++ {
			trimmed := stripped[index]
			if strings.TrimSpace(trimmed) == "" || strings.HasPrefix(strings.TrimSpace(trimmed), "#") {
				continue
			}
			indent := leadingSpaces(trimmed)
			if indent == 0 {
				break // roles 块到此结束
			}
			if strings.TrimSpace(trimmed) == roleKey+":" {
				roleIndent, roleIndex = indent, index
				break
			}
			if isFlowRoleKey(trimmed, roleKey) {
				return nil, fmt.Errorf("seelex: accounts.yaml 的 roles.%s 是行内写法，无法安全插入新条目；请先把该块改成多行列表", roleKey)
			}
		}
	}

	if roleIndex < 0 {
		// 角色块不存在：在 roles 块尾（roles 也缺则整份文件尾）追加一个完整角色块。
		if rolesIndex >= 0 {
			insertAt := rolesEnd(stripped, rolesIndex)
			const blockIndent = 2
			newLines := []string{strings.Repeat(" ", blockIndent) + roleKey + ":"}
			newLines = append(newLines, entry.entryLines(blockIndent)...)
			return joinInsert(stripped, insertAt, newLines, eol, trailingNewline), nil
		}
		newLines := []string{"roles:", "  " + roleKey + ":"}
		newLines = append(newLines, entry.entryLines(2)...)
		return joinInsert(stripped, len(stripped), newLines, eol, trailingNewline), nil
	}

	blockEnd := roleBlockEnd(stripped, roleIndex, roleIndent)
	itemIndent := roleIndent
	for index := roleIndex + 1; index < blockEnd; index++ {
		if strings.HasPrefix(strings.TrimSpace(stripped[index]), "- ") {
			itemIndent = leadingSpaces(stripped[index])
			break
		}
	}
	return joinInsert(stripped, blockEnd, entry.entryLines(itemIndent), eol, trailingNewline), nil
}

// isFlowRoleKey 报告某一行是不是"角色键与值写在同一行"的 flow 写法
// （`agent: [{model: x}]`）：这种形状没法行级插入，必须显式报错而不是插出
// 第二个同名键（那会变成重复键，加载器报错、用户却看不出为什么）。
func isFlowRoleKey(line, roleKey string) bool {
	trimmed := strings.TrimSpace(line)
	prefix := roleKey + ":"
	if !strings.HasPrefix(trimmed, prefix) {
		return false
	}
	return strings.TrimSpace(strings.TrimPrefix(trimmed, prefix)) != ""
}

// rolesEnd 返回 roles 块之后的第一行下标（块内 = 非空、非注释、缩进 > 0）。
func rolesEnd(lines []string, rolesIndex int) int {
	end := rolesIndex + 1
	for index := rolesIndex + 1; index < len(lines); index++ {
		trimmed := strings.TrimSpace(lines[index])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			// 空行与注释算作"块内间隙"：块后面还有内容就继续，真的到文件尾才停。
			continue
		}
		if leadingSpaces(lines[index]) == 0 {
			return index
		}
		end = index + 1
	}
	return end
}

// roleBlockEnd 返回角色块之后的第一行下标。
//
// 一条行属于角色块，当且仅当非空、非注释，且满足其一：缩进大于角色键；或缩进**等于**
// 角色键但自己是序列条目（`  - model:`）——accounts.yaml 的既有形状正是后者（yaml.v3
// 把块序列写在键的同一缩进上）。比角色键更浅、或同缩进却是个兄弟键，就是块结束。
func roleBlockEnd(lines []string, roleIndex, roleIndent int) int {
	end := roleIndex + 1
	for index := roleIndex + 1; index < len(lines); index++ {
		trimmed := strings.TrimSpace(lines[index])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := leadingSpaces(lines[index])
		if indent < roleIndent {
			return index
		}
		if indent == roleIndent && !strings.HasPrefix(trimmed, "- ") {
			return index
		}
		end = index + 1
	}
	return end
}

// joinInsert 把 newLines 插到 insertAt 之前（insertAt == len 即追加到文件尾）。
func joinInsert(lines []string, insertAt int, newLines []string, eol string, trailingNewline bool) []byte {
	merged := make([]string, 0, len(lines)+len(newLines))
	merged = append(merged, lines[:insertAt]...)
	merged = append(merged, newLines...)
	merged = append(merged, lines[insertAt:]...)
	joined := strings.Join(merged, eol)
	if trailingNewline || len(lines) == 0 {
		joined += eol
	}
	return []byte(joined)
}

func leadingSpaces(line string) int {
	spaces := 0
	for spaces < len(line) && line[spaces] == ' ' {
		spaces++
	}
	return spaces
}

// verifyInsertedAccount 用真实加载器复核插入结果，并返回新账号的名字。
//
// 三条判据（任何一条不成立都不写盘）：整份字节可解析；目标角色账号数正好 +1；
// 新账号解析出来的字段与输入逐字相符（这条才真正说明"插进了对的块"）。
func verifyInsertedAccount(updated []byte, before Config, role model.AccountRole, entry pendingAccount) (string, error) {
	after, err := parseAccounts(updated)
	if err != nil {
		return "", fmt.Errorf("seelex: 插入账号后账号档无法解析（已放弃写入）: %w", err)
	}
	beforeCount := countRole(before.Specs, role)
	afterCount := countRole(after.Specs, role)
	if afterCount != beforeCount+1 {
		return "", fmt.Errorf("seelex: 插入校验失败：roles.%s 条目数 %d → %d（期望 %d，已放弃写入）",
			role, beforeCount, afterCount, beforeCount+1)
	}
	name := entry.name
	created := accountSpecByName(after.Specs, name)
	if created == nil {
		return "", fmt.Errorf("seelex: 插入校验失败：解析结果里没有 %q（已放弃写入）", name)
	}
	if created.Model != entry.model || created.APIKey != entry.apiKey || created.BaseURL != entry.baseURL ||
		created.Role != role {
		return "", fmt.Errorf("seelex: 插入校验失败：%q 的字段与输入不符（已放弃写入）", name)
	}
	if entry.provider != "" && created.Provider != entry.provider {
		return "", fmt.Errorf("seelex: 插入校验失败：%q 的 provider 为 %q，期望 %q（已放弃写入）",
			name, created.Provider, entry.provider)
	}
	return name, nil
}

func accountSpecByName(specs []model.AccountSpec, name string) *model.AccountSpec {
	for index := range specs {
		if specs[index].Name == name {
			return &specs[index]
		}
	}
	return nil
}

func nextAccountName(specs []model.AccountSpec, role model.AccountRole) string {
	used := make(map[string]struct{})
	for _, spec := range specs {
		used[spec.Name] = struct{}{}
	}
	for suffix := 1; ; suffix++ {
		name := fmt.Sprintf("%s-%d", role, suffix)
		if _, exists := used[name]; !exists {
			return name
		}
	}
}

func accountRecordFromSpec(spec model.AccountSpec) AccountRecord {
	return AccountRecord{
		Name:            spec.Name,
		Role:            string(spec.Role),
		Provider:        spec.Provider,
		Model:           spec.Model,
		BaseURL:         spec.BaseURL,
		ReasoningEffort: spec.ReasoningEffort,
		APIKeySet:       spec.APIKey != "",
	}
}

func splitAccountLines(raw []byte) ([]string, string, bool) {
	text := string(raw)
	eol := "\n"
	if strings.Contains(text, "\r\n") {
		eol = "\r\n"
	}
	trailing := strings.HasSuffix(text, "\n")
	lines := strings.Split(text, "\n")
	if trailing {
		lines = lines[:len(lines)-1]
	}
	for index := range lines {
		lines[index] = strings.TrimSuffix(lines[index], "\r")
	}
	return lines, eol, trailing
}

func joinAccountLines(lines []string, eol string, trailing bool) []byte {
	text := strings.Join(lines, eol)
	if trailing || len(lines) == 0 {
		text += eol
	}
	return []byte(text)
}

func replaceLineRange(lines []string, start, end int, replacement []string) []string {
	next := make([]string, 0, len(lines)-(end-start)+len(replacement))
	next = append(next, lines[:start]...)
	next = append(next, replacement...)
	next = append(next, lines[end:]...)
	return next
}

func removeLineRange(lines []string, start, end int) []string {
	return replaceLineRange(lines, start, end, nil)
}

// trimItemTail 保留条目与下一条之间的空行和注释（它们可能是下一条的说明），
// 只替换/删除真正属于该账号的内容。
func trimItemTail(lines []string, start, end int) int {
	contentEnd := end
	for contentEnd > start+1 {
		trimmed := strings.TrimSpace(lines[contentEnd-1])
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			break
		}
		contentEnd--
	}
	return contentEnd
}

func locateRoleItems(lines []string, role model.AccountRole) (roleIndex, roleIndent, roleEnd int, starts []int, err error) {
	roleKey := string(role)
	rolesIndex := -1
	for index, line := range lines {
		if line == "roles:" {
			rolesIndex = index
			break
		}
	}
	if rolesIndex < 0 {
		return 0, 0, 0, nil, fmt.Errorf("seelex: accounts.yaml has no roles block")
	}
	roleIndex, roleIndent = -1, -1
	for index := rolesIndex + 1; index < len(lines); index++ {
		trimmed := strings.TrimSpace(lines[index])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := leadingSpaces(lines[index])
		if indent == 0 {
			break
		}
		if trimmed == roleKey+":" {
			roleIndex, roleIndent = index, indent
			break
		}
		if isFlowRoleKey(lines[index], roleKey) {
			return 0, 0, 0, nil, fmt.Errorf("seelex: accounts.yaml 的 roles.%s 是行内写法，无法安全编辑", roleKey)
		}
	}
	if roleIndex < 0 {
		return 0, 0, 0, nil, fmt.Errorf("seelex: accounts.yaml has no roles.%s block", roleKey)
	}
	roleEnd = roleBlockEnd(lines, roleIndex, roleIndent)
	for index := roleIndex + 1; index < roleEnd; index++ {
		if strings.HasPrefix(strings.TrimSpace(lines[index]), "- ") {
			starts = append(starts, index)
		}
	}
	if len(starts) == 0 {
		return 0, 0, 0, nil, fmt.Errorf("seelex: roles.%s has no account entries", roleKey)
	}
	return roleIndex, roleIndent, roleEnd, starts, nil
}

func freezeRoleAccountNames(lines []string, role model.AccountRole, specs []model.AccountSpec) ([]string, error) {
	_, _, roleEnd, starts, err := locateRoleItems(lines, role)
	if err != nil {
		return nil, err
	}
	roleSpecs := make([]model.AccountSpec, 0)
	for _, spec := range specs {
		if spec.Role == role {
			roleSpecs = append(roleSpecs, spec)
		}
	}
	if len(roleSpecs) != len(starts) {
		return nil, fmt.Errorf("seelex: roles.%s 条目数 %d 与解析结果 %d 不一致（已放弃写入）", role, len(starts), len(roleSpecs))
	}
	for index := len(starts) - 1; index >= 0; index-- {
		start := starts[index]
		end := roleEnd
		if index+1 < len(starts) {
			end = starts[index+1]
		}
		if hasAccountField(lines[start:end], "name:") {
			continue
		}
		indent := leadingSpaces(lines[start]) + 2
		nameLine := strings.Repeat(" ", indent) + "name: " + yamlScalar(roleSpecs[index].Name)
		lines = replaceLineRange(lines, start+1, start+1, []string{nameLine})
	}
	return lines, nil
}

func hasAccountField(lines []string, prefix string) bool {
	for _, line := range lines {
		for _, candidate := range []string{prefix, "- " + prefix} {
			if strings.HasPrefix(strings.TrimSpace(line), candidate) {
				return true
			}
		}
	}
	return false
}

func findAccountItem(lines []string, role model.AccountRole, name string) (start, end, itemIndent int, err error) {
	_, _, roleEnd, starts, err := locateRoleItems(lines, role)
	if err != nil {
		return 0, 0, 0, err
	}
	for index, start := range starts {
		end = roleEnd
		if index+1 < len(starts) {
			end = starts[index+1]
		}
		for _, line := range lines[start:end] {
			trimmed := strings.TrimSpace(line)
			value := ""
			switch {
			case strings.HasPrefix(trimmed, "- name:"):
				value = strings.TrimSpace(strings.TrimPrefix(trimmed, "- name:"))
			case strings.HasPrefix(trimmed, "name:"):
				value = strings.TrimSpace(strings.TrimPrefix(trimmed, "name:"))
			}
			if value == "" {
				continue
			}
			if strings.HasPrefix(value, `"`) {
				if unquoted, unquoteErr := strconv.Unquote(value); unquoteErr == nil {
					value = unquoted
				}
			}
			if value == name {
				return start, end, leadingSpaces(lines[start]), nil
			}
		}
	}
	return 0, 0, 0, fmt.Errorf("seelex: account %q not found in roles.%s", name, role)
}

func sameRoleAccountNames(before, after []model.AccountSpec, role model.AccountRole) bool {
	names := func(specs []model.AccountSpec) []string {
		result := make([]string, 0)
		for _, spec := range specs {
			if spec.Role == role {
				result = append(result, spec.Name)
			}
		}
		return result
	}
	a, b := names(before), names(after)
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}

func countRole(specs []model.AccountSpec, role model.AccountRole) int {
	count := 0
	for _, spec := range specs {
		if spec.Role == role {
			count++
		}
	}
	return count
}

// writeFileAtomic 同目录临时文件 + 原子替换（权限沿用原文件的权限位）。
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if mode == 0 {
		mode = 0o600
	}
	temp, err := os.CreateTemp(dir, ".accounts-*.yaml")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath) // 替换成功后它就是空操作
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
