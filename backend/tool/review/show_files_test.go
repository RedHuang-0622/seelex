package review

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// P21-4 域级用例：show_files 的**形状**（名字 / 描述 / schema 上限 / 参数校验 / 回执）
// 随形状一起住在域包。装配层的端到端用例（`backend/engine/assemble/review_files_test.go`）
// 仍经工具面派发，一行未改。

type stubPort struct {
	calls int
	got   []dto.ReviewFileRef
	out   dto.ReviewRequest
	err   error
}

func (port *stubPort) RequestFileReview(_ context.Context, files []dto.ReviewFileRef) (dto.ReviewRequest, error) {
	port.calls++
	port.got = files
	if port.err != nil {
		return dto.ReviewRequest{}, port.err
	}
	if port.out.RequestID == "" {
		port.out = dto.ReviewRequest{RequestID: "review-1", Files: files}
	}
	return port.out, nil
}

func TestShowFilesSchemaCarriesBatchCap(t *testing.T) {
	schema := ShowFilesSchema()
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("schema 缺 properties：%#v", schema)
	}
	files, ok := properties["files"].(map[string]interface{})
	if !ok {
		t.Fatalf("schema 缺 files：%#v", properties)
	}
	if files["maxItems"] != dto.MaxReviewFilesPerRequest || files["minItems"] != 1 {
		t.Fatalf("files 的上下限没回契约常量：%#v", files)
	}
	if required, _ := schema["required"].([]string); len(required) != 1 || required[0] != "files" {
		t.Fatalf("files 必须 required：%#v", schema["required"])
	}
	if !strings.Contains(ShowFilesDescription(), "batch") {
		t.Fatalf("描述应说明「一次调用 = 一次批处理」：%q", ShowFilesDescription())
	}
	if ShowFilesToolName != "show_files" {
		t.Fatalf("工具名被改了：%q", ShowFilesToolName)
	}
}

func TestShowFilesHandlerValidatesBeforeReachingPort(t *testing.T) {
	port := &stubPort{}
	handler := ShowFilesHandler(port)

	// 空清单：显式报错，不打扰落点。
	if _, err := handler(context.Background(), `{"files":[]}`); err == nil {
		t.Fatal("空清单必须报错")
	} else if !strings.Contains(err.Error(), "不能为空") {
		t.Fatalf("空清单文案：%v", err)
	}
	// 坏 JSON：带上 decode 原因。
	if _, err := handler(context.Background(), `{`); err == nil {
		t.Fatal("坏 JSON 必须报错")
	} else if !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("坏 JSON 文案：%v", err)
	}
	// 超限：显式报错（不静默截断）。
	over := make([]dto.ReviewFileRef, 0, dto.MaxReviewFilesPerRequest+1)
	for index := 0; index <= dto.MaxReviewFilesPerRequest; index++ {
		over = append(over, dto.ReviewFileRef{Path: "file.go"})
	}
	encoded, _ := json.Marshal(map[string]interface{}{"files": over})
	if _, err := handler(context.Background(), string(encoded)); err == nil {
		t.Fatal("超限必须报错")
	} else if !strings.Contains(err.Error(), "最多召唤") {
		t.Fatalf("超限文案：%v", err)
	}
	if port.calls != 0 {
		t.Fatalf("形状校验不过时不得惊动落点：calls = %d", port.calls)
	}
}

func TestShowFilesHandlerReturnsReceiptAndPropagatesPortError(t *testing.T) {
	port := &stubPort{out: dto.ReviewRequest{
		RequestID: "review-7",
		Files:     []dto.ReviewFileRef{{Path: "a.go"}},
		Skipped:   []dto.ReviewSkip{{Path: "secret.env", Reason: dto.ReviewSkipSensitive}},
	}}
	raw, err := ShowFilesHandler(port)(context.Background(),
		`{"files":[{"path":"a.go"},{"path":"secret.env"}]}`)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	var receipt ShowFilesReceipt
	if err := json.Unmarshal([]byte(raw), &receipt); err != nil {
		t.Fatalf("回执不是合法 JSON：%v（%s）", err, raw)
	}
	if receipt.Requested != 2 || receipt.Opened != 1 || receipt.RequestID != "review-7" {
		t.Fatalf("回执计数不对：%+v", receipt)
	}
	if len(receipt.Skipped) != 1 || receipt.Skipped[0].Reason != dto.ReviewSkipSensitive {
		t.Fatalf("跳过原因必须如实回报：%+v", receipt.Skipped)
	}

	// 落点失败：原样冒泡（工具面不改判）。
	port.err = errors.New("workspace: 越界")
	if _, err := ShowFilesHandler(port)(context.Background(), `{"files":[{"path":"a.go"}]}`); err == nil ||
		!strings.Contains(err.Error(), "越界") {
		t.Fatalf("落点错误应原样冒泡：%v", err)
	}
}
