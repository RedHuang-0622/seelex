package attachment

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
)

const nonce = "SEELEX-DOC-7731"

func doc(name, mime string, data []byte) types.FilePart {
	return types.FilePart{Kind: types.FileKindDocument, Name: name, MimeType: mime, Data: data}
}

func TestPlanForSeparatesImageAndDocument(t *testing.T) {
	image := types.FilePart{Kind: types.FileKindImage, Name: "shot.png", MimeType: "image/png", Data: []byte{0x89, 'P', 'N', 'G'}}
	plan := PlanFor([]types.FilePart{image, doc("memo.txt", "text/plain", []byte("code is "+nonce))})

	if len(plan.Files) != 1 || plan.Files[0].Name != "shot.png" {
		t.Fatalf("图片应当原样保留给附件通道，实际 Files=%+v", plan.Files)
	}
	if !strings.Contains(plan.Text, nonce) || !strings.Contains(plan.Text, `<attachment name="memo.txt" mime="text/plain">`) {
		t.Fatalf("文档应当内联成带来源标注的文本，实际 Text=%q", plan.Text)
	}
	if strings.Contains(plan.Text, "shot.png") {
		t.Fatalf("图片不该被内联进文本：%q", plan.Text)
	}
	if len(plan.Notes) != 1 || !strings.Contains(plan.Notes[0], "已内联为文本") {
		t.Fatalf("内联也要留一条处置说明，实际 Notes=%v", plan.Notes)
	}
	if !strings.HasPrefix(plan.TextSuffix(), "\n\n") {
		t.Fatalf("TextSuffix 应带空行分隔：%q", plan.TextSuffix())
	}
}

func TestPlanForInlineDecision(t *testing.T) {
	cases := []struct {
		name     string
		file     types.FilePart
		inlined  bool
		noteHas  string
		keepFile bool
	}{
		{"text/plain 内联", doc("memo.txt", "text/plain", []byte("hello "+nonce)), true, "已内联为文本", false},
		{"text/markdown 内联", doc("spec.md", "text/markdown", []byte("# 标题")), true, "已内联为文本", false},
		{"application/json 内联", doc("cfg.json", "application/json", []byte(`{"nonce":"`+nonce+`"}`)), true, "已内联为文本", false},
		{"+json 后缀内联", doc("meta", "application/vnd.api+json", []byte(`{"a":1}`)), true, "已内联为文本", false},
		{"没填 MIME 按内容嗅探", doc("note", "", []byte("plain text "+nonce)), true, "已内联为文本", false},
		{"PDF 不给内联", doc("spec.pdf", "application/pdf", []byte("%PDF-1.4 xx")), false, "不是可直接内联的文本类型", false},
		{"docx 不给内联", doc("spec.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", []byte("PK\x03\x04")), false, "不是可直接内联的文本类型", false},
		{"文本类型但含二进制字节", doc("broken.txt", "text/plain", []byte{0xff, 0xfe, 0x00, 0x01}), false, "非 UTF-8", false},
		{"只有 URL", types.FilePart{Kind: types.FileKindDocument, Name: "spec.pdf", MimeType: "application/pdf", URL: "https://example.com/spec.pdf"}, false, "只有 URL", false},
		{"没有内容", doc("empty.txt", "text/plain", nil), false, "没有内容", false},
		{"超过内联上限", doc("big.txt", "text/plain", []byte(strings.Repeat("a", Limit+1))), false, "超过内联上限", false},
		{"图片不走内联", types.FilePart{Kind: types.FileKindImage, Name: "a.png", MimeType: "image/png", Data: []byte("x")}, false, "", true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			plan := PlanFor([]types.FilePart{testCase.file})
			if got := len(plan.Text) > 0; got != testCase.inlined {
				t.Fatalf("内联判定不符：want %v got %v（Text=%q Notes=%v）", testCase.inlined, got, plan.Text, plan.Notes)
			}
			if got := len(plan.Files) == 1; got != testCase.keepFile {
				t.Fatalf("保留判定不符：want %v got %v（Files=%+v）", testCase.keepFile, got, plan.Files)
			}
			if !testCase.keepFile && testCase.noteHas != "" {
				if len(plan.Notes) != 1 || !strings.Contains(plan.Notes[0], testCase.noteHas) {
					t.Fatalf("诊断应含 %q，实际 Notes=%v", testCase.noteHas, plan.Notes)
				}
			}
			if testCase.keepFile && len(plan.Notes) != 0 {
				t.Fatalf("图片不该产生内联诊断，实际 Notes=%v", plan.Notes)
			}
		})
	}
}

func TestInlineKeepsAttachmentBodyReadable(t *testing.T) {
	body := "第一行\n第二行 " + nonce + "\n\n"
	plan := PlanFor([]types.FilePart{doc(`we"ird.txt`, "text/plain", []byte(body))})
	if !strings.Contains(plan.Text, "第一行\n第二行 "+nonce+"\n</attachment>") {
		t.Fatalf("内联文本应保留正文并去掉尾部空行，实际 %q", plan.Text)
	}
	if strings.Contains(plan.Text, `we"ird.txt`) {
		t.Fatalf("文件名里的引号应被规整，否则会破坏标注：%q", plan.Text)
	}
	if !strings.Contains(plan.Text, `name="we'ird.txt"`) {
		t.Fatalf("规整后的文件名应当出现在标注里：%q", plan.Text)
	}
}

func TestPlanForEmptyInput(t *testing.T) {
	plan := PlanFor(nil)
	if len(plan.Files) != 0 || plan.Text != "" || len(plan.Notes) != 0 || plan.TextSuffix() != "" {
		t.Fatalf("空输入应当空处置，实际 %+v", plan)
	}
}
