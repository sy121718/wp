package aiservice

import (
	"testing"

	aienums "go_wp/internal/module/ai/enums"
)

func TestParseChatReplyReadsReasoningContent(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"role":"assistant","content":"答案是 42",
		"reasoning_content":"先用 orders_summary 确认口径，再算。"}}]}`)
	reply, err := parseProtocolReply(aienums.ProtocolOpenAIChatCompletions, body)
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if reply.Content != "答案是 42" {
		t.Fatalf("正文不对：%q", reply.Content)
	}
	if reply.Reasoning != "先用 orders_summary 确认口径，再算。" {
		t.Fatalf("思考过程没取到：%q", reply.Reasoning)
	}
}

func TestParseChatReplyReadsReasoningAlias(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"role":"assistant","content":"好","reasoning":"想一下"}}]}`)
	reply, err := parseProtocolReply(aienums.ProtocolOpenAIChatCompletions, body)
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if reply.Reasoning != "想一下" {
		t.Fatalf("别名的思考过程没取到：%q", reply.Reasoning)
	}
}

func TestParseChatReplyReasoningAloneIsNotOutput(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"role":"assistant","content":"","reasoning_content":"我在想"}}]}`)
	if _, err := parseProtocolReply(aienums.ProtocolOpenAIChatCompletions, body); err == nil {
		t.Fatal("只有思考过程时应当判失败")
	}
}

func TestParseResponsesReplyReadsReasoningItem(t *testing.T) {
	body := []byte(`{"object":"response","status":"completed","output":[
		{"type":"reasoning","summary":[{"type":"summary_text","text":"先看订单表。"}]},
		{"type":"message","content":[{"type":"output_text","text":"本月 12 单"}]}]}`)
	reply, err := parseProtocolReply(aienums.ProtocolOpenAIResponses, body)
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if reply.Content != "本月 12 单" {
		t.Fatalf("正文不对：%q", reply.Content)
	}
	if reply.Reasoning != "先看订单表。" {
		t.Fatalf("思考过程没取到：%q", reply.Reasoning)
	}
}

func TestParseResponsesReplyReasoningWithTopLevelText(t *testing.T) {
	body := []byte(`{"object":"response","status":"completed","output_text":"结论",
		"output":[{"type":"reasoning","summary":[{"type":"summary_text","text":"推理"}]}]}`)
	reply, err := parseProtocolReply(aienums.ProtocolOpenAIResponses, body)
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if reply.Reasoning != "推理" {
		t.Fatalf("走 output_text 出口时思考过程丢了：%q", reply.Reasoning)
	}
}

func TestParseResponsesReplyJoinsReasoningSegments(t *testing.T) {
	body := []byte(`{"object":"response","status":"completed","output":[
		{"type":"reasoning","summary":[{"type":"summary_text","text":"第一段"}]},
		{"type":"reasoning","content":[{"type":"text","text":"第二段"}]},
		{"type":"message","content":[{"type":"output_text","text":"答"}]}]}`)
	reply, err := parseProtocolReply(aienums.ProtocolOpenAIResponses, body)
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if reply.Reasoning != "第一段\n第二段" {
		t.Fatalf("多段思考没拼好：%q", reply.Reasoning)
	}
}

func TestParseChatReplyWithoutReasoningIsEmpty(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"role":"assistant","content":"好"}}]}`)
	reply, err := parseProtocolReply(aienums.ProtocolOpenAIChatCompletions, body)
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if reply.Reasoning != "" {
		t.Fatalf("没有思考过程时应当是空串，得到：%q", reply.Reasoning)
	}
}
