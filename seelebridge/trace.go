package seelebridge

import "github.com/RedHuang-0622/Seele/seelectx/tracer"

type Trace = tracer.Tracer
type TraceNode = tracer.Node
type TraceTree = tracer.Tree

const (
	SpanLLMCall      = tracer.SpanLLMCall
	SpanToolDispatch = tracer.SpanToolDispatch
	TraceStatusError = tracer.StatusError
)

func NewTracer() Trace { return tracer.NewSimpleTracer() }
