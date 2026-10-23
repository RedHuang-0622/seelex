package registry

// transport_fake_test.go —— **假执行面**：五个能力面都实现，入参与返回值都可编排。
//
// 为什么要一个全能力假面：远端面的判据全在**工具层**（路径不被改写、内容走内容通道、
// 回执自报执行面、超时/截断/仍在跑分开），只有把传输换掉才测得到。真连一台机器既不可
// 控（网络、口令、文件系统）也不该出现在单测里。
//
// 记录是值拷贝：用例断言"远端实际收到了什么"，而不是"handler 说自己收到了什么"。

import (
	"context"
	"sync"
	"sync/atomic"
)

// fakeTransport 是假执行面。零值即"什么都不编排的 ssh 面"（返回空事实、不报错）。
type fakeTransport struct {
	name  string
	calls int32

	mu      sync.Mutex
	execs   []TransportRequest
	reads   []TransportReadRequest
	writes  []TransportWriteRequest
	edits   []TransportEditRequest
	manages []TransportManageRequest

	execOut   TransportOutput
	execErr   error
	readOut   TransportReadOutput
	readErr   error
	writeOut  TransportWriteOutput
	writeErr  error
	editOut   TransportEditOutput
	editErr   error
	manageOut TransportManageOutput
	manageErr error
}

func (f *fakeTransport) Kind() TransportKind { return TransportSSH }

// Calls 返回五个能力面被调用的总次数（选择器用例用"零调用"钉住"权限拒绝不连远端"）。
func (f *fakeTransport) Calls() int { return int(atomic.LoadInt32(&f.calls)) }

func (f *fakeTransport) record() {
	atomic.AddInt32(&f.calls, 1)
}

func (f *fakeTransport) Exec(_ context.Context, req TransportRequest) (TransportOutput, error) {
	f.record()
	f.mu.Lock()
	f.execs = append(f.execs, req)
	f.mu.Unlock()
	out := f.execOut
	if out.Status == "" {
		out.Status = "completed"
	}
	if out.Stdout == "" {
		out.Stdout = f.name
	}
	return out, f.execErr
}

func (f *fakeTransport) Read(_ context.Context, req TransportReadRequest) (TransportReadOutput, error) {
	f.record()
	f.mu.Lock()
	f.reads = append(f.reads, req)
	f.mu.Unlock()
	return f.readOut, f.readErr
}

func (f *fakeTransport) Write(_ context.Context, req TransportWriteRequest) (TransportWriteOutput, error) {
	f.record()
	f.mu.Lock()
	f.writes = append(f.writes, req)
	f.mu.Unlock()
	out := f.writeOut
	if out.Bytes == 0 {
		out.Bytes = len(req.Content)
	}
	return out, f.writeErr
}

func (f *fakeTransport) Edit(_ context.Context, req TransportEditRequest) (TransportEditOutput, error) {
	f.record()
	f.mu.Lock()
	f.edits = append(f.edits, req)
	f.mu.Unlock()
	return f.editOut, f.editErr
}

func (f *fakeTransport) Manage(_ context.Context, req TransportManageRequest) (TransportManageOutput, error) {
	f.record()
	f.mu.Lock()
	f.manages = append(f.manages, req)
	f.mu.Unlock()
	return f.manageOut, f.manageErr
}

// 下列读面给用例用（锁内取快照，避免用例与 handler 竞争）。

func (f *fakeTransport) execCalls() []TransportRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]TransportRequest(nil), f.execs...)
}

func (f *fakeTransport) readCalls() []TransportReadRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]TransportReadRequest(nil), f.reads...)
}

func (f *fakeTransport) writeCalls() []TransportWriteRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]TransportWriteRequest(nil), f.writes...)
}

func (f *fakeTransport) editCalls() []TransportEditRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]TransportEditRequest(nil), f.edits...)
}

func (f *fakeTransport) manageCalls() []TransportManageRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]TransportManageRequest(nil), f.manages...)
}

// sshContext 造一个"这次调用已选中 ssh"的执行 ctx（选择器的产出形状）。
//
// 用例直接构造选择结果而不是跑整套探针矩阵：探针/降级由 transport_selector_test.go
// 负责，这里只判"选完之后远端面做了什么"。
func sshContext(transport Transport, projectID, env string) context.Context {
	return WithTransportSelection(context.Background(), TransportSelection{
		Kind: TransportSSH, Transport: transport, ProjectID: projectID,
		Endpoint: "deploy.internal:22", Env: env, Reason: ReasonProbeOK,
	})
}
