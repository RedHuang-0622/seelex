package assemble

// runtime_teamwork_scope.go — 团队面"会话号 → 后端与作用域键"的**唯一**解析点。
//
// 生态位：这是宿主的**读数接线**，不是域判据。会话 → (project_id, session_id) 的映射由
// 组合根注入的 `TeamworkBackend.KeyFor` 决定，域包不认识"当前装的是哪个后端"。本文件只回答
// "现在装配的团队面认不认得这个会话、键是否完整"。
//
// 为什么收成一份（P21 收尾）：这段解析此前在 `coordinatorFor` / `coordinatorForSession` /
// `teamSceneIndex` / `teamUnitScope` / `teamworkReadScope` / `archiveTeamBoard` /
// `TeamworkBoardSnapshot` / `CloseTeamBoard` / 看板域的 `KeyFor` 接线里各写了一遍，而且已经
// 漂移：两处只看 `KeyFor` 的 ok、不看键的两个分量，半截键因此可以走到存储层；`teamworkKeyFor`
// 又漏了 trim。现在所有读法都问这一格，判据只剩 `TeamworkBackend.KeyFor` + `dto.SessionKey.Complete`。

import (
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/Seele/jobs"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

var (
	// errTeamworkNotAssembled 是"团队面未装配"的唯一报错（宿主读数缺一格）。
	errTeamworkNotAssembled = errors.New("teamwork: 未装配（缺 SetTeamworkBackend）")
	// errTeamworkNoSession 是"没有会话归属"的唯一报错。读法同时服务工具调用与后台尾插，
	// 因此这里只陈述事实，不假设调用来自 team_* 回合。
	errTeamworkNoSession = errors.New("teamwork: 缺少会话归属（主会话号为空）")
)

// resolveTeamworkScope 解析当前团队面的三格读数：后端、作业表、作用域键。
//
// 判据（唯一一份）：会话号 trim 后非空 → 后端与 KeyFor 已装配 → KeyFor 认得这个会话 →
// 键的两个分量齐全（`dto.SessionKey.Complete`）。任何一条不成立都返回零值 + 理由；
// 调用方按自己的语义决定"退场 / 报错 / 回落"，不再各自重判。
func (r *Runtime) resolveTeamworkScope(sessionID string) (*TeamworkBackend, jobs.Manager, dto.SessionKey, error) {
	if r == nil {
		return nil, nil, dto.SessionKey{}, errTeamworkNotAssembled
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, nil, dto.SessionKey{}, errTeamworkNoSession
	}
	r.teamworkMu.Lock()
	backend, manager := r.teamworkBackend, r.teamworkJobs
	r.teamworkMu.Unlock()
	if backend == nil || backend.KeyFor == nil {
		return nil, nil, dto.SessionKey{}, errTeamworkNotAssembled
	}
	key, ok := backend.KeyFor(sessionID)
	if !ok || !key.Complete() {
		return nil, nil, dto.SessionKey{}, fmt.Errorf("teamwork: 无法解析会话 %q 的作用域键（项目/会话）", sessionID)
	}
	return backend, manager, key, nil
}

// teamworkKeyFor 是 resolveTeamworkScope 的 ok 形态（未装配 / 认不出 / 半截键 ⇒ false）。
//
// 它供只关心"能不能定位会话"的读面使用（看板域的两格读数之一）。判据仍只在上面那一处：
// 这里只把理由折成 ok，不另判一次。
func (r *Runtime) teamworkKeyFor(sessionID string) (dto.SessionKey, bool) {
	_, _, key, err := r.resolveTeamworkScope(sessionID)
	if err != nil {
		return dto.SessionKey{}, false
	}
	return key, true
}
