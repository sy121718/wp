package aiservice

import (
	"strings"
	"testing"
)

func feedChat(t *testing.T, acc *streamAccumulator, lines ...string) []StreamDelta {
	t.Helper()
	out := make([]StreamDelta, 0, len(lines))
	for _, line := range lines {
		d, done := feedChatChunk([]byte(line), acc)
		if d.Text != "" || d.Reasoning != "" || done {
			out = append(out, d)
		}
	}
	return out
}

func TestStreamChatAccumulatesText(t *testing.T) {
	acc := newStreamAccumulator()
	feedChat(t, acc,
		`{"choices":[{"delta":{"content":"本月"}}]}`,
		`{"choices":[{"delta":{"content":"共 12"}}]}`,
		`{"choices":[{"delta":{"content":"单"}}]}`,
		`[DONE]`,
	)
	if got := acc.Reply().Content; got != "本月共 12单" {
		t.Fatalf("正文累加不对：%q", got)
	}
	if !acc.done {
		t.Fatal("收到 [DONE] 后 done 应当为真")
	}
}

func TestStreamChatAccumulatesReasoningSeparately(t *testing.T) {
	acc := newStreamAccumulator()
	feedChat(t, acc,
		`{"choices":[{"delta":{"reasoning_content":"先看"}}]}`,
		`{"choices":[{"delta":{"reasoning_content":"订单表。"}}]}`,
		`{"choices":[{"delta":{"content":"12 单"}}]}`,
	)
	reply := acc.Reply()
	if reply.Reasoning != "先看订单表。" {
		t.Fatalf("思考过程累加不对：%q", reply.Reasoning)
	}
	if reply.Content != "12 单" {
		t.Fatalf("思考过程串进正文了：%q", reply.Content)
	}
}

func TestStreamChatMergesToolCallsByIndex(t *testing.T) {
	acc := newStreamAccumulator()
	feedChat(t, acc,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"name":"orders_summary"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_b","function":{"name":"orders_top_products"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"range\":"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"week\"}"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{}"}}]}}]}`,
	)
	reply := acc.Reply()
	if len(reply.ToolCalls) != 2 {
		t.Fatalf("工具调用条数不对：%d", len(reply.ToolCalls))
	}
	if reply.ToolCalls[0].Name != "orders_summary" || reply.ToolCalls[0].Arguments != `{"range":"week"}` {
		t.Fatalf("第一个调用拼错：%+v", reply.ToolCalls[0])
	}
	if reply.ToolCalls[1].Name != "orders_top_products" || reply.ToolCalls[1].Arguments != "{}" {
		t.Fatalf("第二个调用拼错：%+v", reply.ToolCalls[1])
	}
	if reply.ToolCalls[0].ID != "call_a" || reply.ToolCalls[1].ID != "call_b" {
		t.Fatalf("id 丢了：%+v", reply.ToolCalls)
	}
}

func TestStreamChatDropsNamelessCall(t *testing.T) {
	acc := newStreamAccumulator()
	feedChat(t, acc,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{}"}}]}}]}`,
		`{"choices":[{"delta":{"content":"好"}}]}`,
	)
	if got := acc.Reply().ToolCalls; len(got) != 0 {
		t.Fatalf("无名的调用不该留下：%+v", got)
	}
}

func TestStreamChatReadsUsageFromTailChunk(t *testing.T) {
	acc := newStreamAccumulator()
	feedChat(t, acc,
		`{"choices":[{"delta":{"content":"答"}}]}`,
		`{"choices":[],"usage":{"prompt_tokens":120,"completion_tokens":8,"total_tokens":128,"prompt_tokens_details":{"cached_tokens":96}}}`,
		`[DONE]`,
	)
	reply := acc.Reply()
	if !reply.Usage.Reported {
		t.Fatal("尾片的 usage 没读到")
	}
	if reply.Usage.InputTokens != 120 || reply.Usage.CachedTokens != 96 {
		t.Fatalf("用量解析不对：%+v", reply.Usage)
	}
	if !reply.Usage.CachedReported {
		t.Fatal("cached_tokens 报了 96 但没被标为已上报")
	}
}

func TestStreamChatSkipsBadChunk(t *testing.T) {
	acc := newStreamAccumulator()
	feedChat(t, acc,
		`{"choices":[{"delta":{"content":"前"}}]}`,
		`this is not json`,
		``,
		`{"choices":[{"delta":{"content":"后"}}]}`,
	)
	if got := acc.Reply().Content; got != "前后" {
		t.Fatalf("坏片让内容丢了：%q", got)
	}
}

func TestStreamChatReasoningAloneIsNotOutput(t *testing.T) {
	acc := newStreamAccumulator()
	feedChat(t, acc, `{"choices":[{"delta":{"reasoning_content":"我在想"}}]}`)
	reply := acc.Reply()
	if strings.TrimSpace(reply.Content) != "" || len(reply.ToolCalls) != 0 {
		t.Fatalf("不应判为有产出：%+v", reply)
	}
}

func TestStreamResponsesEvents(t *testing.T) {
	acc := newStreamAccumulator()
	feed := func(event, data string) StreamDelta {
		d, _ := feedResponsesEvent(event, []byte(data), acc)
		return d
	}
	feed("response.created", `{"type":"response.created"}`)
	if d := feed(responsesEventReasoningDelta, `{"delta":"先看订单。"}`); d.Reasoning != "先看订单。" {
		t.Fatalf("思考过程增量没读到：%+v", d)
	}
	if d := feed(responsesEventTextDelta, `{"delta":"本月 12 单"}`); d.Text != "本月 12 单" {
		t.Fatalf("正文增量没读到：%+v", d)
	}
	d, done := feedResponsesEvent(responsesEventCompleted,
		[]byte(`{"type":"response.completed","usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}`), acc)
	if !done || !d.Done {
		t.Fatal("completed 事件没被当成结束")
	}
	reply := acc.Reply()
	if reply.Content != "本月 12 单" || reply.Reasoning != "先看订单。" {
		t.Fatalf("汇合结果不对：%+v", reply)
	}
	if reply.Usage.TotalTokens != 15 {
		t.Fatalf("结束事件的用量没读到：%+v", reply.Usage)
	}
}

func TestStreamResponsesCollectsToolCalls(t *testing.T) {
	acc := newStreamAccumulator()
	feed := func(event, data string) {
		feedResponsesEvent(event, []byte(data), acc)
	}
	feed("response.output_item.added", `{"type":"response.output_item.added","item":{"type":"function_call","name":"site_projects","call_id":"c1"}}`)
	feed(responsesEventTextDelta, `{"type":"response.output_text.delta","delta":"正在查…"}`)

	feed(responsesEventCompleted, `{"type":"response.completed","response":{`+
		`"output":[`+
		`{"type":"function_call","name":"order_find","call_id":"c1","arguments":"{\"projectId\":\"p1\"}"},`+
		`{"type":"function_call","name":"order_get","call_id":"c2","arguments":"{\"orderNo\":\"N1\"}"}`+
		`],`+
		`"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}`)

	reply := acc.Reply()
	if len(reply.ToolCalls) != 2 {
		t.Fatalf("应收到 2 个工具调用，实得 %d：%+v", len(reply.ToolCalls), reply.ToolCalls)
	}
	if reply.ToolCalls[0].Name != "order_find" || reply.ToolCalls[0].ID != "c1" {
		t.Errorf("第一个调用拼错了：%+v", reply.ToolCalls[0])
	}
	if reply.ToolCalls[0].Arguments != `{"projectId":"p1"}` {
		t.Errorf("第一个调用的参数没取到：%+v", reply.ToolCalls[0])
	}
	if reply.ToolCalls[1].Name != "order_get" {
		t.Errorf("第二个调用拼错了：%+v", reply.ToolCalls[1])
	}
	if reply.Content != "正在查…" {
		t.Errorf("正文应当照常累加，实得 %q", reply.Content)
	}

	if !reply.Usage.Reported || reply.Usage.TotalTokens != 15 {
		t.Errorf("用量应从 response.usage 读出，实得 %+v", reply.Usage)
	}
	if !acc.done {
		t.Error("completed 之后 done 应当为真")
	}
}

func TestStreamResponsesBackfillsTextFromCompleted(t *testing.T) {
	acc := newStreamAccumulator()
	delta, done := feedResponsesEvent(responsesEventCompleted, []byte(`{"type":"response.completed","response":{`+
		`"output":[`+
		`{"type":"reasoning","summary":[{"type":"summary_text","text":"先看看能做什么"}]},`+
		`{"type":"message","role":"assistant","content":[{"type":"output_text","text":"我可以查订单、客户、库存和流量。"}]}`+
		`],`+
		`"usage":{"input_tokens":100,"output_tokens":221,"total_tokens":321}}}`), acc)

	if !done {
		t.Fatal("completed 应标记流程结束")
	}
	reply := acc.Reply()
	if !strings.Contains(reply.Content, "我可以查订单") {
		t.Fatalf("正文应从 output[] 回填，实得 %q", reply.Content)
	}
	if !strings.Contains(reply.Reasoning, "先看看能做什么") {
		t.Fatalf("思考过程也应回填，实得 %q", reply.Reasoning)
	}

	if !strings.Contains(delta.Text, "我可以查订单") {
		t.Errorf("回填的正文应随事件推出去，实得 %q", delta.Text)
	}
	if !strings.Contains(delta.Reasoning, "先看看能做什么") {
		t.Errorf("回填的思考应随事件推出去，实得 %q", delta.Reasoning)
	}
}

func TestStreamResponsesDoesNotDuplicateWhenDeltaPresent(t *testing.T) {
	acc := newStreamAccumulator()
	feed := func(event, data string) { feedResponsesEvent(event, []byte(data), acc) }
	feed(responsesEventTextDelta, `{"type":"response.output_text.delta","delta":"一遍"}`)
	feed(responsesEventCompleted, `{"type":"response.completed","response":{`+
		`"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"一遍"}]}],`+
		`"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`)

	if got := acc.Reply().Content; got != "一遍" {
		t.Fatalf("有 delta 时不该重复回填，实得 %q", got)
	}
}

func TestParseResponsesOutputTextSkipsOtherParts(t *testing.T) {
	output := []any{
		map[string]any{"type": "reasoning", "summary": []any{map[string]any{"text": "想"}}},
		map[string]any{"type": "function_call", "name": "order_find", "arguments": "{}"},
		map[string]any{"type": "message", "content": []any{
			map[string]any{"type": "output_text", "text": "第一段"},
			map[string]any{"type": "refusal", "text": "不该出现"},
		}},
		map[string]any{"type": "message", "content": []any{
			map[string]any{"type": "output_text", "text": "第二段"},
		}},
	}
	got := parseResponsesOutputText(output)
	if got != "第一段\n第二段" {
		t.Fatalf("应只拼 output_text 段并以换行连接，实得 %q", got)
	}
	if strings.Contains(got, "想") || strings.Contains(got, "不该出现") {
		t.Errorf("不该把思考或拒答段混进正文：%q", got)
	}
}
