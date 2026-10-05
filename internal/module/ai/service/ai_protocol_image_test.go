package aiservice

// ai_protocol_image_test.go — 两族协议的图片分片形状。
//
// 两族的形状**不一样**，而且错法很隐蔽：把 url 写成对象（或写成字符串）
// 都不会让请求失败 —— 上游按未知结构忽略掉那个分片，请求 200，模型只是
// 「看不到图」。所以这里逐个字段钉住，而不是只断言「有个数组」。

import (
	"encoding/json"
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

// 不带图时 content 必须是**字符串**：纯文本用数组是这条协议里较弱的形态，
// 部分网关对它的处理是「模型收到的正文变成奇怪的东西」。
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

// 带图时 chat 的分片是 {"type":"image_url","image_url":{"url":...}}（**url 是对象**）。
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
	// url 必须是**对象**：写成字符串这一片会被静默忽略。
	urlObj, ok := imgPart["image_url"].(map[string]any)
	if !ok {
		t.Fatalf("image_url 应当是对象，实得 %T", imgPart["image_url"])
	}
	if urlObj["url"] != imgURI {
		t.Fatalf("图片地址没带对：%+v", urlObj)
	}
}

// 带图时 responses 的分片是 {"type":"input_image","image_url":"<字符串>"}（**url 是字符串**）。
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
	// url 必须是**字符串**（这一族与 chat 相反）：写成对象同样会被静默忽略。
	if imgPart["image_url"] != imgURI {
		t.Fatalf("image_url 应当是字符串形态，实得 %T：%+v", imgPart["image_url"], imgPart["image_url"])
	}
}

// 带图的 user 消息不能退化成裸字符串（那会让图片被静默丢掉）。
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
