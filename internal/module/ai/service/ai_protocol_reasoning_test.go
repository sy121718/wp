package aiservice

// ai_protocol_reasoning_test.go — 思考过程（reasoning）的解析。
//
// 为什么值得单独钉：**它失败时页面完全正常**。字段名认不出 → Reasoning 为空 →
// 悬浮球不显示那一段，用户看到的是「这个功能不存在」；日志里只有一次成功的调用。
// 而字段名在两族协议里本来就不一样（chat 用消息上的 reasoning_content / reasoning，
// responses 用 output[] 里与 message 并列的 reasoning 条目），换一家供应商就会踩到，
// 假上游的用例也不会替我们发现。

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

// 只有思考过程、没有正文也没有工具调用 = 这一轮没产出，仍须判失败。
// 反过来（把 Reasoning 也算作产出）会让外层拿一个空回答去写会话事件。
func TestParseChatReplyReasoningAloneIsNotOutput(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"role":"assistant","content":"","reasoning_content":"我在想"}}]}`)
	if _, err := parseProtocolReply(aienums.ProtocolOpenAIChatCompletions, body); err == nil {
		t.Fatal("只有思考过程时应当判失败")
	}
}

// responses 的思维链是 output[] 里与 message 并列的一种条目，不在 message 上。
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

// 顶层 output_text 那条捷径出口也必须带上思考过程：
// 漏掉它时，同一次回答「走哪条取值路径」会决定思考过程显不显示。
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

// 多段思考要拼起来：模型一次可能给出几段，只留最后一段会让人只看到结论前的半句。
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

// 上游return 空串/缺字段时 Reasoning 必须是空，而不是报错 ——
// 「这家不返回思考过程」是常态，不是故障。
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
