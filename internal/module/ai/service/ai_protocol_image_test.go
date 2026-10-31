package aiservice

import (
	json "go_wp/pkg/jsonx"
	"testing"

	aidto "go_wp/internal/module/ai/dto"
)

const imgURI = "data:image/png;base64,AAAA"

func decodeImageBody(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("请求体不是合法 JSON：%v", err)
	}
	return out
}

func TestChatBodyKeepsPlainStringWithoutImages(t *testing.T) {
	body, err := buildChatCompletionsBody("m", []aidto.ChatMessage{{Role: "user", Content: "你好"}}, nil, 0, false)
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}
	msgs, _ := decodeImageBody(t, body)["messages"].([]any)
	item, _ := msgs[0].(map[string]any)
	if _, isStr := item["content"].(string); !isStr {
		t.Fatalf("无图时 content 应当是字符串，实得 %T", item["content"])
	}
}

func TestChatBodySplitsImagesAsParts(t *testing.T) {
	msgs := []aidto.ChatMessage{{Role: "user", Content: "这是什么", Images: []string{imgURI}}}
	body, err := buildChatCompletionsBody("m", msgs, nil, 0, false)
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}
	list, _ := decodeImageBody(t, body)["messages"].([]any)
	item, _ := list[0].(map[string]any)
	parts, ok := item["content"].([]any)
	if !ok {
		t.Fatalf("带图时 content 应当是分片数组，实得 %T", item["content"])
	}
	if len(parts) != 2 {
		t.Fatalf("应当是「文本 + 图片」两片，实得 %d 片", len(parts))
	}
	textPart, _ := parts[0].(map[string]any)
	if textPart["type"] != "text" || textPart["text"] != "这是什么" {
		t.Fatalf("第一片应是正文，实得 %+v", textPart)
	}
	imgPart, _ := parts[1].(map[string]any)
	if imgPart["type"] != "image_url" {
		t.Fatalf("第二片应是图片，实得 %+v", imgPart)
	}

	urlObj, ok := imgPart["image_url"].(map[string]any)
	if !ok {
		t.Fatalf("image_url 应当是对象，实得 %T", imgPart["image_url"])
	}
	if urlObj["url"] != imgURI {
		t.Fatalf("图片地址没带对：%+v", urlObj)
	}
}

func TestResponsesBodySplitsImagesAsParts(t *testing.T) {
	msgs := []aidto.ChatMessage{
		{Role: roleSystem, Content: "规则"},
		{Role: roleUser, Content: "这是什么", Images: []string{imgURI}},
	}
	body, err := buildResponsesBody("m", msgs, nil, 0, false)
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}
	root := decodeImageBody(t, body)
	if root["instructions"] != "规则" {
		t.Fatalf("system 应当被抽到顶层 instructions，实得 %v", root["instructions"])
	}
	list, _ := root["input"].([]any)
	if len(list) != 1 {
		t.Fatalf("抽走 system 后应只剩一条 user，实得 %d 条", len(list))
	}
	item, _ := list[0].(map[string]any)
	parts, ok := item["content"].([]any)
	if !ok {
		t.Fatalf("带图时 content 应当是分片数组，实得 %T", item["content"])
	}
	if len(parts) != 2 {
		t.Fatalf("应当是「文本 + 图片」两片，实得 %d 片", len(parts))
	}
	textPart, _ := parts[0].(map[string]any)
	if textPart["type"] != "input_text" || textPart["text"] != "这是什么" {
		t.Fatalf("第一片应是正文，实得 %+v", textPart)
	}
	imgPart, _ := parts[1].(map[string]any)
	if imgPart["type"] != "input_image" {
		t.Fatalf("第二片应是图片，实得 %+v", imgPart)
	}

	if imgPart["image_url"] != imgURI {
		t.Fatalf("image_url 应当是字符串形态，实得 %T：%+v", imgPart["image_url"], imgPart["image_url"])
	}
}

func TestResponsesPlainMessageExcludesImages(t *testing.T) {
	plain := aidto.ChatMessage{Role: roleUser, Content: "你好"}
	if !isPlainMessage(plain) {
		t.Fatal("无图的 user 消息应当可以退成裸字符串")
	}
	withImage := aidto.ChatMessage{Role: roleUser, Content: "你好", Images: []string{imgURI}}
	if isPlainMessage(withImage) {
		t.Fatal("带图的消息不能退成裸字符串 —— 那样图片会被上游静默忽略")
	}
}
