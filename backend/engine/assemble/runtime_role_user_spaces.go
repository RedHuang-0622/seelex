package assemble

import "strings"

// SetRoleUserSpacesProvider 注入“角色在远端用户空间允许访问的路径”读面。
// 它只在角色回合进入工具链时读取；不缓存为进程级当前值，避免并发角色互相覆盖。
func (r *Runtime) SetRoleUserSpacesProvider(provider func(sessionID, role string) []string) {
	if r == nil {
		return
	}
	r.roleUserSpacesMu.Lock()
	r.roleUserSpaces = provider
	r.roleUserSpacesMu.Unlock()
}

func (r *Runtime) roleUserSpacesFor(sessionID, role string) []string {
	if r == nil {
		return nil
	}
	r.roleUserSpacesMu.RLock()
	provider := r.roleUserSpaces
	r.roleUserSpacesMu.RUnlock()
	if provider == nil {
		return nil
	}
	paths := provider(strings.TrimSpace(sessionID), strings.TrimSpace(role))
	return append([]string{}, paths...)
}
