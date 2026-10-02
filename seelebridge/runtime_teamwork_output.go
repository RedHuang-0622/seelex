package seelebridge

// runtime_teamwork_output.go — teammate 作业**输出文件**的产品面（S5 / §4.7 输出归属）。
//
// 要解决的问题（改之前的事实）：worker 作业的输出文件由框架自建
// （`<outputDir>/<handle>.log`），而框架在**销项 / 驱逐 / Close** 三处都会删掉它。于是
// leader 按规范做"阶段收尾取回产出"（jobs_manage op=fetch，取尽即销项）的那一刻，正文
// 就跟着没了——而团队收口（team_close）之前，那些正文正是看板、team_context 与收口核对
// 的证据。框架侧为此提供了 `jobs.Spec.OutputPath`：非空 = **产品自有文件**，框架不建写
// 句柄、只按偏移读，且 `externalOutput.remove()` 是空操作（永不删）。
//
// 本文件就是那个"产品侧"：
//   - 目录：会话目录内 `<sessionRoot>/teamwork/jobs`（跟着会话走；会话被清理时一并消失，
//     不需要第二套回收机制）；
//   - 文件名：每次派发一个（`<role>-<n>.log`，进程内单调），互不覆盖——同一角色的第 n 轮
//     正文不会被第 n+1 轮truncate 掉；
//   - 生命周期：整队收口（team_close）时**整目录清掉**。收口就是产品决定"不再需要"的
//     那一刻，也就是"正文活到 close"这条口径的终点。清目录**与写入串行**（同一把锁）
//     并同时作废这一批落点：被取消回合的最后一次写不会在清目录之后造出残文件。
//   - 回读：作业行被框架 prune 逐出后（框架没有 pin 概念），按句柄读不到正文；读面据
//     本面按**角色名**回读最近一份（LatestJobOutputPath），残边因此不再等于"正文丢了"。
//
// 与框架自建形态的分工（两条路都要能跑）：未装配本面 = `Spec.OutputPath` 留空，框架自建并
// 按框架语义在销项 / 驱逐 / Close 时删除；装配了 = 归产品。两种形态下 jobs 的读法
// （Observe / Peek / Fetch 按偏移读）逐字相同——这正是 `outputStore` 两种形状的意义。

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
	"github.com/RedHuang-0622/seelex/seelebridge/teamwork"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// TeamworkJobOutputs 实现 teamwork.JobOutputs：把"分配 / 写入 / 清理 / 回读" teammate
// 作业输出文件落在会话存储的 teamwork 模块目录上。
type TeamworkJobOutputs struct {
	store  sessionstore.TeamworkRepository
	keyFor func(sessionID string) (sessionstore.Key, bool)

	// mu 同时守着 seq（序号）与 live（在册落点），**并且跨文件 I/O 持有**：WriteJobOutput
	// 与 ClearJobOutputs 因此串行——收口清目录不会与"被取消回合的最后一次写"并发（S5 残边）。
	mu sync.Mutex
	// seq 是会话 ID → 该会话已分配的文件序号（进程内单调；重启清零不影响读，见 JobOutputPath）。
	seq map[string]int
	// live 是"落点路径 → 会话 ID"：JobOutputPath 登记、ClearJobOutputs 作废。写入前查它，
	// 于是已经作废（被收口清掉）的落点上不会再有迟到的写把文件造出来。
	live map[string]string
}

// NewTeamworkJobOutputs 装配产品自有输出面。store / keyFor 缺失时返回 nil：调用方据此
// 不把它交给 teamwork 编排面（缺失 = 交回框架自建，不是降级成半残实现）。
func NewTeamworkJobOutputs(store sessionstore.TeamworkRepository, keyFor func(sessionID string) (sessionstore.Key, bool)) *TeamworkJobOutputs {
	if store == nil || keyFor == nil {
		return nil
	}
	return &TeamworkJobOutputs{store: store, keyFor: keyFor, seq: map[string]int{}, live: map[string]string{}}
}

// JobOutputPath 分配一次派发的输出文件路径（调用方把它同时交给执行体与
// jobs.Spec.OutputPath，见 WorkerRequest.OutputPath 的说明）。
//
// 序号只在**进程内**单调：重启后从 1 重新开始，于是理论上可能与上一个进程留下的同名
// 文件撞车。这不是问题——那些文件属于上一次进程的团队，而收口会清掉整目录；即使撞上，
// 也只是覆盖一份上一轮的正文，不会让任何**在册**的读法读到过期内容（读法按游标与
// stats 走，文件是新的就是新的）。
//
// 进程内**不复用**落点：收口清目录（ClearJobOutputs）不复位序号，于是同一个路径不会在
// 一个进程里被第二个回合重新登记——否则"作废的落点"会重新变成有效，旧回合的迟到写就会
// 打到新团队的同名文件上。
func (o *TeamworkJobOutputs) JobOutputPath(ctx context.Context, role string) (string, error) {
	key, sessionID, err := o.resolve(ctx)
	if err != nil {
		return "", err
	}
	dir, err := o.store.TeamworkJobOutputDir(ctx, key)
	if err != nil {
		return "", err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.seq[sessionID]++
	index := o.seq[sessionID]
	path := filepath.Join(dir, fmt.Sprintf("%s-%d.log", safeRoleFileName(role), index))
	if o.live == nil {
		o.live = map[string]string{}
	}
	o.live[path] = sessionID
	return path, nil
}

// WriteJobOutput 把一轮正文写进产品自有输出文件。它是执行体写正文的**唯一入口**。
//
// 为什么要经产品面而不是执行体各自 os.WriteFile：收口清目录（ClearJobOutputs）可能与
// "被取消回合的最后一次写"并发——Reclaim 等待执行体收尾有上限（默认 5s），超时后它就
// 返回，而那一轮的正文可能还在路上。两者经同一个 o.mu 串行，且清目录会作废这一批落点，
// 于是在任何一种交错下目录都不会留下残文件（S5 残边，devlog §4.3）：
//
//   - 写先拿到锁 → 文件写出来；随后清目录把它删掉；
//   - 清目录先拿到锁 → 落点作废；随后的写查不到落点，**静默丢弃**（不是失败：产品已经
//     决定不再需要这份正文）。
func (o *TeamworkJobOutputs) WriteJobOutput(path, text string) error {
	if o == nil {
		return errors.New("teamwork: 作业输出面未装配")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("teamwork: 写作业输出需要落点")
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, ok := o.live[path]; !ok {
		// 落点不在册：已被收口清掉（或根本不是本面分配的路径）。这一写属于一个已经
		// 作废的回合，丢弃它就是这条残边的修法——把它写出来才是在造残文件。
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("teamwork: 创建作业输出目录失败: %w", err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		return fmt.Errorf("teamwork: 写作业输出失败: %w", err)
	}
	return nil
}

// ClearJobOutputs 清掉当前会话那一批作业输出文件（整队收口调用）。
//
// 清的是**整个 jobs 目录**：目录里只有本会话 teammate 作业的正文，而收口是"这支团队不再
// 需要它们的正文"——删单个文件要维护一份"哪些文件属于本轮团队"的名册，那是第二份状态。
// 目录不存在（没派过活 / 已清过）= 无事可做，不是失败。
//
// 清目录与"作废这一批落点"是同一个临界区里的一件事：删掉文件的同时把 live 里属于本会话
// 的落点一并移除，于是并发/迟到的写（WriteJobOutput）查不到落点而被丢弃——这才是"绝对
// 干净"的保证，而不是赌"清得快、写在它之前"。
func (o *TeamworkJobOutputs) ClearJobOutputs(ctx context.Context) error {
	key, sessionID, err := o.resolve(ctx)
	if err != nil {
		return err
	}
	dir, err := o.store.TeamworkJobOutputDir(ctx, key)
	if err != nil {
		return err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("teamwork: 清作业输出目录失败: %w", err)
	}
	for path, owner := range o.live {
		if owner == sessionID {
			delete(o.live, path)
		}
	}
	// 序号刻意**不复位**：进程内路径不复用，见 JobOutputPath。
	return nil
}

// LatestJobOutputPath 按**角色名**定位该角色最近一份产品自有正文（`<role>-<n>.log` 里 n
// 最大的那份），返回它的路径；ok=false = 该角色没有正文文件。
//
// 它服务于一条残边（devlog §4.2）：作业行是内存态、会被框架 prune 逐出（框架没有 pin
// 概念），逐出之后按句柄读不到任何东西——而正文文件归产品、活到收口。读面据此按角色名
// 回读，而不是把"行不在册"冒充成"从没写过正文"。
func (o *TeamworkJobOutputs) LatestJobOutputPath(ctx context.Context, role string) (string, bool, error) {
	key, _, err := o.resolve(ctx)
	if err != nil {
		return "", false, err
	}
	dir, err := o.store.TeamworkJobOutputDir(ctx, key)
	if err != nil {
		return "", false, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("teamwork: 读作业输出目录失败: %w", err)
	}
	prefix := safeRoleFileName(role) + "-"
	best, bestIndex := "", -1
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".log") {
			continue
		}
		index, err := strconv.Atoi(strings.TrimSuffix(name[len(prefix):], ".log"))
		if err != nil {
			continue
		}
		if index > bestIndex {
			bestIndex, best = index, filepath.Join(dir, name)
		}
	}
	if best == "" {
		return "", false, nil
	}
	return best, true, nil
}

// readOutputHead 读一个产品自有输出文件的前 limit 字节（limit<=0 = 读全）。文件不存在 =
// 空串（正文可能还没落盘，不是错误）。它只服务于读面的回读（§4.2 残边）：不消费、不改事实。
func readOutputHead(path string, limit int) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	defer file.Close()
	if limit <= 0 {
		data, err := io.ReadAll(file)
		return string(data), err
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// resolve 解析当前调用的（会话作用域键, 会话 ID）。会话归属与 jobs 的解析同源
// （telemetry 的 SessionIDFromContext），不引入第二套"当前会话"判据。
func (o *TeamworkJobOutputs) resolve(ctx context.Context) (sessionstore.Key, string, error) {
	if o == nil || o.store == nil || o.keyFor == nil {
		return sessionstore.Key{}, "", errors.New("teamwork: 作业输出面未装配")
	}
	sessionID := strings.TrimSpace(telemetry.SessionIDFromContext(ctx))
	if sessionID == "" {
		return sessionstore.Key{}, "", errors.New("teamwork: 当前调用没有会话归属")
	}
	key, ok := o.keyFor(sessionID)
	if !ok || strings.TrimSpace(key.ProjectID) == "" || strings.TrimSpace(key.SessionID) == "" {
		return sessionstore.Key{}, "", fmt.Errorf("teamwork: 无法解析会话 %q 的作用域键", sessionID)
	}
	return key, sessionID, nil
}

// safeRoleFileName 把角色名折成安全的文件名片段：角色名进路径，就必须先过这一关
// （`..` / 分隔符 / 盘符都不能走到文件系统上）。空结果退回 "role"——名字不合法不该让
// 整个流程失败，但也不能让路径退化成目录本身。
func safeRoleFileName(role string) string {
	trimmed := strings.TrimSpace(role)
	if trimmed == "" {
		return "role"
	}
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '-', r == '_', r == '.':
			return r
		default:
			return '_'
		}
	}, trimmed)
	if strings.Trim(cleaned, ".") == "" {
		return "role"
	}
	return cleaned
}

var _ teamwork.JobOutputs = (*TeamworkJobOutputs)(nil)
