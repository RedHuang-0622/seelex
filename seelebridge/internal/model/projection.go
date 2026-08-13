package model

import "github.com/RedHuang-0622/seelex/application/contract/dto"

// ParentEvidenceProjection 是 application 到 Runtime 的最小父证据投影：
// Runtime 据此构造子代理可读的父证据快照（合并回传的起点）。
type ParentEvidenceProjection = dto.ParentEvidenceProjection
