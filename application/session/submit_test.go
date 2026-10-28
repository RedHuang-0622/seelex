package session

import (
	"context"
	"errors"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// imageSuffix 把随行图片压成断言用的小尾巴（没有图 = 空串，行为不变）。
func imageSuffix(images []dto.ComposerImage) string {
	if len(images) == 0 {
		return ""
	}
	suffix := "+images"
	for _, image := range images {
		suffix += ":" + image.Ref
	}
	return suffix
}

// TestSubmitApplicationCarriesImagesToSubmitter 证明带图入口与纯文本入口走同一份
// 准入顺序，且图片原样交到提交口（图片属于这一条输入，不能在中途丢）。
func TestSubmitApplicationCarriesImagesToSubmitter(t *testing.T) {
	var got []dto.ComposerImage
	application := NewSubmit(SubmitDeps{
		Materialize: func(string, string) error { return nil },
		Loaded:      func(string) (bool, bool) { return true, false },
		Resume:      func(string) error { return nil },
		Submit: func(_ context.Context, _ string, _ string, images []dto.ComposerImage) error {
			got = images
			return nil
		},
	})
	images := []dto.ComposerImage{{Ref: "media:aaa", Name: "shot.png", MimeType: "image/png", Bytes: 12}}
	if err := application.SubmitToSessionWithImages(context.Background(), "s1", "看一看", images); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Ref != "media:aaa" {
		t.Fatalf("images = %+v", got)
	}
}

func TestSubmitApplicationMaterializesAndRoutesExplicitSession(t *testing.T) {
	var calls []string
	loaded := false
	contentUnloaded := false
	application := NewSubmit(SubmitDeps{
		Materialize: func(sessionID, text string) error {
			calls = append(calls, "materialize:"+sessionID+":"+text)
			return nil
		},
		Loaded: func(sessionID string) (bool, bool) {
			calls = append(calls, "loaded:"+sessionID)
			return loaded, contentUnloaded
		},
		Resume: func(sessionID string) error {
			calls = append(calls, "resume:"+sessionID)
			return nil
		},
		Submit: func(_ context.Context, sessionID, text string, images []dto.ComposerImage) error {
			calls = append(calls, "submit:"+sessionID+":"+text+imageSuffix(images))
			return nil
		},
	})

	if err := application.SubmitToSession(context.Background(), " s1 ", " hello "); err != nil {
		t.Fatal(err)
	}
	want := []string{"materialize:s1: hello ", "loaded:s1", "resume:s1", "submit:s1: hello "}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v", calls)
	}
	for index := range want {
		if calls[index] != want[index] {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	}

	calls = nil
	loaded, contentUnloaded = true, false
	if err := application.SubmitToSession(context.Background(), "s1", "next"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 || calls[1] != "loaded:s1" || calls[2] != "submit:s1:next" {
		t.Fatalf("loaded calls = %v", calls)
	}
}

func TestSubmitApplicationStopsOnMaterializeFailure(t *testing.T) {
	materializeErr := errors.New("materialize failed")
	application := NewSubmit(SubmitDeps{
		Materialize: func(string, string) error { return materializeErr },
	})
	if err := application.SubmitToSession(context.Background(), "s1", "text"); !errors.Is(err, materializeErr) {
		t.Fatalf("err = %v", err)
	}
	if err := application.SubmitToSession(context.Background(), " ", "text"); err == nil {
		t.Fatal("empty session ID must fail")
	}
}
