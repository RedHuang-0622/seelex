package session

import "errors"

// 会话粒度深拷贝工具（thin-wrapper-session-design.md §5）：fork = 以
// session:<parent> 的 record 前缀（身份/标题/状态/绑定）+ 上下文栈为拷贝
// 面，落到新 session:<child>；执行态全新（引擎句柄不复制）。拷贝面引用
// 不相交（T2.7/B4）：改子不影响父，共享面仅只读常量。

// DeepCopyContextStack 深拷贝上下文四栈（C_i 拷贝面；切片逐层复制）。
func DeepCopyContextStack(src ContextStack) ContextStack {
	return ContextStack{
		Plan:    append([]string(nil), src.Plan...),
		Task:    append([]string(nil), src.Task...),
		Skill:   append([]string(nil), src.Skill...),
		Compact: append([]string(nil), src.Compact...),
	}
}

// DeepCopyRecordPrefix 深拷贝会话记录前缀（record 前缀 = 身份/种类/parent/
// 标题/状态/checkpoint/绑定；不含引擎句柄与运行态）。
func DeepCopyRecordPrefix(src SessionRecord) SessionRecord {
	return SessionRecord{
		ID:         src.ID,
		Kind:       src.Kind,
		ParentID:   src.ParentID,
		Title:      src.Title,
		Status:     src.Status,
		Checkpoint: src.Checkpoint,
		UpdatedAt:  src.UpdatedAt,
		Binding: SessionBinding{
			WorkspaceID: src.Binding.WorkspaceID,
			ParentID:    src.Binding.ParentID,
			Kind:        src.Binding.Kind,
		},
	}
}

// ForkDeepCopy 从父单元创建 fork 子单元：View/Queue/Context 深拷贝
// （引擎句柄 E 全新，不复制父 bundle；B4 共享面仅只读常量）。
func ForkDeepCopy(parent *SessionUnit, childID string, opts ...func(*SessionUnit)) (*SessionUnit, error) {
	if parent == nil {
		return nil, errors.New("session: fork parent is nil")
	}
	child, err := NewSessionUnit(childID)
	if err != nil {
		return nil, err
	}
	child.Kind = parent.Kind
	child.ParentID = parent.ParentID
	child.Title = parent.Title
	child.Binding = parent.Binding
	contextCopy := ContextStack{}
	if parent.Context != nil {
		contextCopy = *parent.Context
	}
	child.Context = &contextCopy
	*child.Context = DeepCopyContextStack(*child.Context)
	if parent.View != nil {
		clone := parent.View.Clone()
		child.View = clone
	}
	for _, apply := range opts {
		if apply != nil {
			apply(child)
		}
	}
	return child, nil
}
