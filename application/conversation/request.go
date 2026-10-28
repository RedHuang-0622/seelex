package conversation

import (
	"strings"

	"github.com/RedHuang-0622/seelex/application/prompt"
	sessioncase "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// Request is the immutable accepted-turn payload.
type Request struct {
	DisplayInput string
	ModelInput   string
	Skills       []prompt.PromptLayer
	Budget       prompt.ReActBudget
	// Images 是这次输入随行附上的图片（输入框拖拽 / 粘贴）。它跟着 Request
	// 一起走完整条队列：入队时随 Payload 存下，晋升时按批次合并，起始时写进
	// 这条 user 事件行——图片因此永远属于"用户发的那一句话"，而不是"当时
	// 队列里恰好有谁"。
	Images []dto.ComposerImage
}

// RequestDeps supplies model-envelope parsing and wrapping rules owned by the
// existing Skill context path.
type RequestDeps struct {
	ParseModelEnvelope func(input string) (display string, bodyOffset int, ok bool)
	WrapModelInput     func(display, body string) string
}

// RequestApplication owns request construction and queue-batch merging.
type RequestApplication struct {
	deps RequestDeps
}

// NewRequestApplication constructs the request factory.
func NewRequestApplication(deps RequestDeps) *RequestApplication {
	return &RequestApplication{deps: deps}
}

// NewRequest freezes one accepted input and its Skill layers (no images).
//
// 从耐久队列恢复的输入只带正文（落盘的队列行不带图片），恢复路径因此不补图：
// 宁可"重启后这条排队消息没有缩略图"，也不给一条旧消息挂上不属于它的图。
func (application *RequestApplication) NewRequest(input string, layers []prompt.PromptLayer) Request {
	return application.NewRequestWithImages(input, layers, nil)
}

// NewRequestWithImages 冻结一条**带图**输入：图片随这条请求一起走完整条队列。
//
// 用两个入口而不是把 images 塞进所有调用方：纯文本调用方（耐久队列恢复、命令路由）
// 只关心正文，它们不该为"多一个 nil"改签名；带图的入口只有提交那一条路。
func (application *RequestApplication) NewRequestWithImages(
	input string,
	layers []prompt.PromptLayer,
	images []dto.ComposerImage,
) Request {
	display := strings.TrimSpace(input)
	return Request{
		DisplayInput: display,
		ModelInput:   display,
		Skills:       SelectedSkillLayers(layers),
		Images:       normalizeRequestImages(images),
	}
}

// normalizeRequestImages 收下一条输入随行的图片：去空 ref、按 ref 去重并保序。
//
// 为什么去重放在这里：同一次提交里不该出现两条同 ref（内容寻址下就是同一张图），
// 前端已经去过一次，但请求是跨层载荷——到了这一层仍然只按**唯一判据**（ref）判，
// 免得未来第二个调用方送进重复条目让模型看两遍。
func normalizeRequestImages(images []dto.ComposerImage) []dto.ComposerImage {
	if len(images) == 0 {
		return nil
	}
	out := make([]dto.ComposerImage, 0, len(images))
	seen := make(map[string]struct{}, len(images))
	for _, image := range images {
		ref := strings.TrimSpace(image.Ref)
		if ref == "" {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		image.Ref = ref
		out = append(out, image)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// QueuedRequests restores valid request payloads from opaque queue rows.
func (*RequestApplication) QueuedRequests(requests []sessioncase.QueuedRequest) []Request {
	out := make([]Request, 0, len(requests))
	for _, request := range requests {
		if payload, ok := request.Payload.(Request); ok {
			out = append(out, payload)
		}
	}
	return out
}

// Combine merges a promoted queue batch with the existing separator and Skill
// merge semantics.
func (application *RequestApplication) Combine(requests []Request) Request {
	displays := make([]string, 0, len(requests))
	models := make([]string, 0, len(requests))
	decorated := false
	combined := Request{}
	for _, request := range requests {
		displays = append(displays, request.DisplayInput)
		_, bodyOffset, ok := application.deps.ParseModelEnvelope(request.ModelInput)
		if ok {
			decorated = true
			models = append(models, request.ModelInput[bodyOffset:])
		} else {
			models = append(models, request.ModelInput)
		}
	}
	display := strings.Join(displays, "\n---\n")
	model := strings.Join(models, "\n---\n")
	if decorated {
		model = application.deps.WrapModelInput(display, model)
	}
	combined.DisplayInput = display
	combined.ModelInput = model
	for _, request := range requests {
		combined.Skills = mergeSkillLayers(combined.Skills, request.Skills)
	}
	// 图片跟正文同批次合并：排队期间用户可能先后附过图，晋升成一条消息时它们
	// 全属于这一句（正文也是这么拼起来的），因此按送入顺序铺开、按 ref 去重。
	for _, request := range requests {
		combined.Images = append(combined.Images, request.Images...)
	}
	combined.Images = normalizeRequestImages(combined.Images)
	return combined
}

// ChatRequestDisplays extracts display inputs.
func ChatRequestDisplays(requests []Request) []string {
	displays := make([]string, len(requests))
	for index, request := range requests {
		displays[index] = request.DisplayInput
	}
	return displays
}

// QueuedInputRefs extracts non-empty display inputs.
func QueuedInputRefs(queue []Request) []string {
	refs := make([]string, 0, len(queue))
	for _, request := range queue {
		if input := strings.TrimSpace(request.DisplayInput); input != "" {
			refs = append(refs, input)
		}
	}
	return refs
}

// SelectedSkillLayers filters frozen Skill layers.
func SelectedSkillLayers(layers []prompt.PromptLayer) []prompt.PromptLayer {
	skilled := make([]prompt.PromptLayer, 0, len(layers))
	for _, layer := range layers {
		if layer.Kind == "skill" {
			skilled = append(skilled, layer)
		}
	}
	return skilled
}

func mergeSkillLayers(current, incoming []prompt.PromptLayer) []prompt.PromptLayer {
	for _, layer := range incoming {
		found := false
		for index := range current {
			if current[index].Name == layer.Name {
				current[index] = layer
				found = true
				break
			}
		}
		if !found {
			current = append(current, layer)
		}
	}
	return current
}
