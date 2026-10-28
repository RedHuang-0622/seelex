package integration

import "errors"

func (runtime *fakeRuntime) BindProjectRootFor(sessionID, rootPath string) error {
	if sessionID == "" {
		return errors.New("session ID is required")
	}
	runtime.sessionProjectRootsMu.Lock()
	defer runtime.sessionProjectRootsMu.Unlock()
	if runtime.sessionProjectRoots == nil {
		runtime.sessionProjectRoots = make(map[string]string)
	}
	runtime.sessionProjectRoots[sessionID] = rootPath
	return nil
}

func (runtime *fakeRuntime) UnbindProjectRootFor(sessionID string) {
	runtime.sessionProjectRootsMu.Lock()
	defer runtime.sessionProjectRootsMu.Unlock()
	delete(runtime.sessionProjectRoots, sessionID)
}
