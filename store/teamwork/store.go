package teamwork

// store.go — teamwork 的**落盘 owner**：计划整份替换、审计/绑定只追加、崩溃残尾读取。
//
// 物理事实（与 M8 之前逐字相同，路径零迁移）：
//
//   - 计划 = 模块 head `<sessionRoot>/metadata/teamwork.json`，整份替换、原子发布；
//   - 审计 = `<sessionRoot>/teamwork/events.jsonl`，只追加、不重写；
//   - 绑定 = `<sessionRoot>/teamwork/worktrees.jsonl`，只追加、不重写（KV：按
//     work_item 取最后一行，折叠规则在 model.go 的 Bindings）；
//   - 作业输出目录 = `<sessionRoot>/teamwork/jobs`。
//
// 锁身份：计划发布与两条追加流水共用 layout 的 `Lock(scope, ModuleID)`——与根覆盖
// 包原来的 `mu(key, moduleTeamwork)` 是同一把锁，跨适配层不会分裂成两把。
//
// 崩溃残尾：追加写入是"一行 JSON + '\n'"，崩溃只会留下半行；读侧跳过**最后一行**
// 的解析失败（与 compact.jsonl / message 分片同一口径），中间行损坏仍显式报错。

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
	storelayout "github.com/RedHuang-0622/seelex/store/internal/layout"
	storeroot "github.com/RedHuang-0622/seelex/store/root"
)

// ModuleID 是计划 head 的模块身份（metadata/teamwork.json）。
const ModuleID storelayout.ModuleID = "teamwork"

// 落盘名字：与 M8 之前逐字相同，改一个字节就是数据迁移。
const (
	DirName     = "teamwork"
	EventFile   = "events.jsonl"
	BindingFile = "worktrees.jsonl"
	JobsDirName = "jobs"
)

// Store 是 teamwork 持久化面。layout 是组合根持有的共享机制实例。
type Store struct {
	layout *storelayout.Layout
}

// New 构造 teamwork 持久化面。
func New(layout *storelayout.Layout) *Store {
	return &Store{layout: layout}
}

// Dir 返回 teamwork 数据目录（不创建）。
func (s *Store) Dir(scope dto.SessionKey) string {
	return filepath.Join(s.layout.SessionRoot(layoutScope(scope)), DirName)
}

// EventPath 返回审计流水路径。
func (s *Store) EventPath(scope dto.SessionKey) string {
	return filepath.Join(s.Dir(scope), EventFile)
}

// BindingPath 返回绑定账本路径。
func (s *Store) BindingPath(scope dto.SessionKey) string {
	return filepath.Join(s.Dir(scope), BindingFile)
}

// JobsDir 返回作业输出目录（不创建）。
func (s *Store) JobsDir(scope dto.SessionKey) string {
	return filepath.Join(s.Dir(scope), JobsDirName)
}

// WritePlan 整份替换计划 head。maxTeammates <= 0 = 只做结构性校验。
func (s *Store) WritePlan(scope dto.SessionKey, plan Plan, maxTeammates int) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if err := ValidatePlan(plan, maxTeammates); err != nil {
		return err
	}
	return s.layout.Commit(layoutScope(scope), ModuleID, "", plan, time.Now().UTC())
}

// ReadPlan 读回计划。**缺失 = 空计划**（还没声明过团队），不是错误。
//
// 走与其它模块 head 相同的自愈读：首次不匹配重读一次，持续失败才在模块锁内重试
// （计划没有独立数据文件，没有可重建的 payload——repair 为空）。
func (s *Store) ReadPlan(scope dto.SessionKey) (Plan, error) {
	if err := scope.Validate(); err != nil {
		return Plan{}, err
	}
	head, err := s.layout.ReadWithHeal(
		layoutScope(scope),
		ModuleID,
		false,
		func() *sync.Mutex { return s.layout.Lock(layoutScope(scope), ModuleID) },
		nil,
		nil,
	)
	if err != nil {
		if storelayout.IsNotExist(err) {
			return Plan{}, nil
		}
		return Plan{}, err
	}
	return decodePayload[Plan](head)
}

// AppendEvent 追加一条审计事件（只追加、不重写）。
func (s *Store) AppendEvent(scope dto.SessionKey, event Event) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(event.Kind) == "" {
		return errors.New("teamwork: event.kind is required")
	}
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}
	line, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("teamwork: encode event: %w", err)
	}
	return s.appendLine(scope, s.EventPath(scope), line, "audit log")
}

// ReadEvents 读回全部审计事件（文件顺序 = 发生顺序）。
func (s *Store) ReadEvents(scope dto.SessionKey) ([]Event, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return readRows[Event](s.EventPath(scope), "audit")
}

// AppendBinding 追加一行 Work Item 绑定事实（只追加、不重写）。
func (s *Store) AppendBinding(scope dto.SessionKey, binding Binding) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(binding.WorkItem) == "" {
		return errors.New("teamwork: binding.work_item is required")
	}
	if binding.At == 0 {
		binding.At = time.Now().UTC().Unix()
	}
	line, err := json.Marshal(binding)
	if err != nil {
		return fmt.Errorf("teamwork: encode binding: %w", err)
	}
	return s.appendLine(scope, s.BindingPath(scope), line, "binding log")
}

// ReadBindings 读回全部绑定事实（文件顺序 = 发生顺序）。
func (s *Store) ReadBindings(scope dto.SessionKey) ([]Binding, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return readRows[Binding](s.BindingPath(scope), "binding")
}

// JobOutputDir 返回（并确保存在）teammate 作业输出目录。
//
// 目录在会话目录内，跟着会话一起被清理；文件名与生命周期归调用方（存储层不决定
// "哪一轮的输出该留着"）。
func (s *Store) JobOutputDir(scope dto.SessionKey) (string, error) {
	if err := scope.Validate(); err != nil {
		return "", err
	}
	dir := s.JobsDir(scope)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("teamwork: create job output dir: %w", err)
	}
	return dir, nil
}

// appendLine 是两条追加流水的**唯一**物理实现：确保 guide → 模块锁 → 建目录 →
// O_APPEND 单次写一行。label 只用于错误文案（audit log / binding log）。
func (s *Store) appendLine(scope dto.SessionKey, path string, line []byte, label string) error {
	if _, err := s.layout.EnsureGuide(layoutScope(scope)); err != nil {
		return err
	}
	lock := s.layout.Lock(layoutScope(scope), ModuleID)
	lock.Lock()
	defer lock.Unlock()
	if err := os.MkdirAll(s.Dir(scope), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("teamwork: open %s: %w", label, err)
	}
	defer file.Close()
	if _, err := file.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("teamwork: append %s: %w", label, err)
	}
	return nil
}

func layoutScope(key dto.SessionKey) storelayout.Scope {
	return storelayout.Scope{ProjectID: key.ProjectID, SessionID: key.SessionID}
}

// readRows 是两条追加流水的**唯一**读取实现：缺失 = 空切片；跳过崩溃残尾。
func readRows[T any](path, label string) ([]T, error) {
	data, err := storeroot.ReadSharedFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []T{}, nil
		}
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	rows := make([]T, 0, len(lines))
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		var row T
		if err := json.Unmarshal([]byte(trimmed), &row); err != nil {
			if index == len(lines)-1 {
				continue // 崩溃残尾
			}
			return nil, fmt.Errorf("teamwork: decode %s row %d: %w", label, index, err)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// decodePayload 解模块 head 的 payload（与根 storeEngine 的通用解码同一文案）。
func decodePayload[T any](head storelayout.Head) (T, error) {
	var out T
	if len(head.Payload) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(head.Payload, &out); err != nil {
		return out, fmt.Errorf("session storage: decode %s head payload: %w", head.ModuleID, err)
	}
	return out, nil
}
