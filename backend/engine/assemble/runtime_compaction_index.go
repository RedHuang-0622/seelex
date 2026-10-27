package assemble

// runtime_compaction_index.go 是**装配层压缩推帧接收侧**的宿主转发面。
//
// 落点沿革（P21「域方法随类型走」，plan §2.5 的 `runtime_compaction_index` →
// `backend/engine/context`）：判据、状态与帧助手都搬进域包
// [`backend/engine/context/compaction_stack.go`](../context/compaction_stack.go)；
// 这里只留**一行薄转发**与两个类型别名——`Runtime` 的公开方法名与签名逐字不变
// （契约 §2.1），`wiring/adapters` 的窄转发因此零改动。
//
// 宿主面按函数值端口注入（四个读取面都是本包的私有访问器，域包不能反向依赖装配层）：
// `sessionContextStore` / `MainCompactionDAG` / `compactionSummarizer` / `getTurnArchiver`。

import (
	"context"
	"errors"

	context_runtime "github.com/RedHuang-0622/seelex/kernel/context"
)

// errCompactionStackUnassembled 是未装配压缩栈时的错误（文案与原实现逐字一致：
// 域包在"宿主读数缺失"时给同一句）。
var errCompactionStackUnassembled = errors.New("compaction index: runtime is unavailable")

// CompactionFrameRequest / CompactionFrameReceipt 是域包类型的**同身份别名**
// （调用方与既有用例零改动；不是新类型）。
type CompactionFrameRequest = context_runtime.CompactionFrameRequest
type CompactionFrameReceipt = context_runtime.CompactionFrameReceipt

// compactionStack 返回压缩推帧接收侧的域句柄（装配点在 Runtime 构造器里，
// 见 runtime.go 的 `compactionStack` 字段）。
func (r *Runtime) compactionStackHandle() *context_runtime.CompactionStack {
	if r == nil {
		return nil
	}
	return r.compactionStack
}

// CompactionSummaryAvailable 报告装配层压缩这次能不能拿到模型生成的读后感
// （域包 `CompactionStack.SummaryAvailable`：探针语义与"只有出了读后感才更新
// compact stack top"的用户口径见那里）。
func (r *Runtime) CompactionSummaryAvailable() bool {
	stack := r.compactionStackHandle()
	if stack == nil {
		return false
	}
	return stack.SummaryAvailable()
}

// ReadbackCompactionSummary 在压缩之前试一次前缀重放厚摘要（域包 `CompactionStack.Readback`）。
func (r *Runtime) ReadbackCompactionSummary(
	ctx context.Context,
	sessionID string,
	request CompactionFrameRequest,
) (CompactionFrameReceipt, error) {
	stack := r.compactionStackHandle()
	if stack == nil {
		return CompactionFrameReceipt{}, errCompactionStackUnassembled
	}
	return stack.Readback(ctx, sessionID, request)
}

// PushCompactionFrame 生成一帧并压入该会话的压缩栈（域包 `CompactionStack.Push`）。
func (r *Runtime) PushCompactionFrame(
	ctx context.Context,
	sessionID string,
	request CompactionFrameRequest,
) (CompactionFrameReceipt, error) {
	stack := r.compactionStackHandle()
	if stack == nil {
		return CompactionFrameReceipt{}, errCompactionStackUnassembled
	}
	return stack.Push(ctx, sessionID, request)
}
