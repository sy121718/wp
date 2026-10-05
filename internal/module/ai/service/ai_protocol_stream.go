package aiservice

// ai_protocol_stream.go — 两族协议的**流式**增量解析。
//
// 为什么要有这一层：出站改成流式后，上游不再给一个完整 JSON，而是一行行 SSE。
// 每行的形状随协议不同，而「怎么把碎片拼成一次回答」这件事**只有这里知道** ——
// 摊到调用方去拼，工具调用（按 index 分片的 arguments）与正文（逐字）两套拼法
// 会被复制到每个消费者里，然后各自出各自的错。
//
// 拼装规则（两族协议共同的坑）：
//
//  1. 正文是**逐字**来的，必须累加；只留最后一片会让回答只剩最后一个字。
//  2. 工具调用的 arguments 也是**逐片**来的，而且按 index 分组 ——
//     一个 delta 里可能同时出现 index 0 的 name 与 index 1 的 arguments。
//     不按 index 归位、直接把 arguments 字符串首尾相连，拼出来的 JSON 会在
//     两个调用之间粘在一起（解析失败，报的是「工具参数不是合法 JSON」，
//     看不出是拼装的问题）。
//  3. usage 只在**最后一两片**里出现（chat 要显式开 stream_options.include_usage），
//     中间片没有它属正常，不能用「某片没有 usage」推断「这家不上报用量」——
//     那个结论只能在流结束时下。
//  4. [DONE] 是 chat 的结束标记；responses 用 response.completed 事件。

import (
	"encoding/json"
	"strings"

	aidto "go_wp/internal/module/ai/dto"
)

// StreamDelta 一次流式增量。
//
// Text 与 Reasoning 分开给：它们的消费者不同（前者进正文，后者进「思考过程」折叠区），
// 合成一个字符串再让调用方自己切，等于把「这属于哪一边」的知识推给每个消费者。
type StreamDelta struct {
	// Text 本片新增的正文（可能为空：只有思考过程或只有工具调用片段时）。
	Text string
	// Reasoning 本片新增的思考过程。
	Reasoning string
	// Done 流已结束（收到 [DONE] 或 completed 事件）。
	Done bool
	// Usage 结束时上游上报的用量（中间片为零值，Reported=false）。
	Usage ReplyUsage
}

// streamAccumulator 把碎片拼成一次完整回答。
//
// 与 ProtocolReply 同形：流式与非流式最终必须产出同一个结构，
// 否则会话层会出现「用流式时工具调用丢了」这种只在一条路径上出现的缺陷。
type streamAccumulator struct {
	text      strings.Builder
	reasoning strings.Builder
	calls     map[int]*aidto.ToolCall
	order     []int
	usage     ReplyUsage
	done      bool
}

func newStreamAccumulator() *streamAccumulator {
	return &streamAccumulator{calls: map[int]*aidto.ToolCall{}}
}

// Reply 汇总成与非流式相同的结果。
//
// 空判断与非流式一致（parseChatCompletionsReply / parseResponsesReply）：
// **只有思考过程不算产出** —— 那是模型在想，不是它对用户说的话。
func (a *streamAccumulator) Reply() ProtocolReply {
	calls := make([]aidto.ToolCall, 0, len(a.order))
	for _, idx := range a.order {
		call := a.calls[idx]
		if call == nil || strings.TrimSpace(call.Name) == "" {
			// 与 parseChatToolCalls 同一条规矩：没有工具名的调用执行不了，
			// 留着只会让上层拿着「查不到这个工具」的错误去问模型，白烧一轮。
			continue
		}
		calls = append(calls, *call)
	}
	return ProtocolReply{
		Content:   a.text.String(),
		Reasoning: strings.TrimSpace(a.reasoning.String()),
		ToolCalls: calls,
		Usage:     a.usage,
	}
}

// isSilent 判断这一轮**什么都没收到**：没有结束标记、也没有任何正文/思考/工具调用。
//
// 这是「上游其实回了非流式」与「上游换了我们不认识的事件名」共同的形状，
// 两者都不能当成「模型没话说」静默收场 —— 前者可以救回来，后者必须报错。
func (a *streamAccumulator) isSilent() bool {
	return !a.done && a.text.Len() == 0 && a.reasoning.Len() == 0 && len(a.calls) == 0
}

// adoptProtocolReply 把一份非流式解析结果灌进累积器（见 rescueNonStream）。
//
// 只用于「流式什么都没读出来、但整段响应其实是一份完整 JSON」这一种情况：
// 早退标记一并置上，免得后续的「流没结束」判断再把它判成失败。
func (a *streamAccumulator) adoptProtocolReply(reply ProtocolReply) {
	a.text.Reset()
	a.text.WriteString(reply.Content)
	a.reasoning.Reset()
	a.reasoning.WriteString(reply.Reasoning)
	for i, c := range reply.ToolCalls {
		a.mergeCallPart(i, c.ID, c.Name, c.Arguments)
	}
	if reply.Usage.Reported {
		a.usage = reply.Usage
	}
	a.done = true
}

// mergeCallPart 把一片工具调用增量并进累积器。
//
// index 是归位的唯一依据（上游不保证 id/name 与 arguments 出现在同一片里）。
// 名字分片出现时按顺序拼接：有网关把长名字切开（罕见但存在），
// 直接覆盖会只剩最后一段。
func (a *streamAccumulator) mergeCallPart(index int, id, name, args string) {
	call, ok := a.calls[index]
	if !ok {
		call = &aidto.ToolCall{}
		a.calls[index] = call
		a.order = append(a.order, index)
	}
	if id != "" {
		call.ID = id
	}
	if name != "" {
		call.Name += name
	}
	if args != "" {
		call.Arguments += args
	}
}

// feedChatChunk 吃一片 chat/completions 的 SSE data，产出本片的增量。
//
// data 是 `data: ` 之后的内容（不含前缀）。返回 done=true 时 data 通常是 `[DONE]`。
func feedChatChunk(data []byte, acc *streamAccumulator) (StreamDelta, bool) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return StreamDelta{}, false
	}
	if trimmed == streamDoneMarker {
		acc.done = true
		return StreamDelta{Done: true, Usage: acc.usage}, true
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(trimmed), &root); err != nil {
		// 坏片**跳过而不是中断**：流到一半出现一行解析不了的负载（心跳、注释、网关
		// 自己加的行）时，整次回答已经产出的部分不该被丢掉。
		return StreamDelta{}, false
	}
	// usage 可能出现也可能不出现；出现就覆盖累积值（有些网关在中间片给累计用量）。
	if raw, ok := root["usage"]; ok && raw != nil {
		if u := usageFromJSON(raw); u.Reported {
			acc.usage = u
		}
	}
	choices, _ := root["choices"].([]any)
	if len(choices) == 0 {
		return StreamDelta{}, false
	}
	first, _ := choices[0].(map[string]any)
	delta, _ := first["delta"].(map[string]any)
	if delta == nil {
		// 部分网关把结束片写成 message 而不是 delta（不在流式规范里，但见过）。
		delta, _ = first["message"].(map[string]any)
	}
	if delta == nil {
		return StreamDelta{}, false
	}

	text, _ := delta["content"].(string)
	reasoning := firstNonEmptyString(delta, "reasoning_content", "reasoning")
	if text != "" {
		acc.text.WriteString(text)
	}
	if reasoning != "" {
		acc.reasoning.WriteString(reasoning)
	}
	for _, part := range asAnyList(delta["tool_calls"]) {
		obj, ok := part.(map[string]any)
		if !ok {
			continue
		}
		fn, _ := obj["function"].(map[string]any)
		idx := 0
		switch v := obj["index"].(type) {
		case float64:
			idx = int(v)
		case int:
			idx = v
		}
		id, _ := obj["id"].(string)
		name, _ := fn["name"].(string)
		args, _ := fn["arguments"].(string)
		acc.mergeCallPart(idx, id, name, args)
	}
	return StreamDelta{Text: text, Reasoning: reasoning}, false
}

// feedResponsesEvent 吃一片 responses 协议的 SSE（event + data），产出本片的增量。
//
// 这一族把「发生了什么」放在事件名里而不是负载里，所以 event 必须一起看：
// 只看 data 的话 response.completed 与一个普通 delta 长得一样。
func feedResponsesEvent(event string, data []byte, acc *streamAccumulator) (StreamDelta, bool) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return StreamDelta{}, false
	}
	if trimmed == streamDoneMarker {
		acc.done = true
		return StreamDelta{Done: true, Usage: acc.usage}, true
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(trimmed), &root); err != nil {
		return StreamDelta{}, false
	}
	if event == "" {
		event, _ = root["type"].(string)
	}
	// responses 把真正的数据放在 response 子对象里：
	// {"type":"response.completed","response":{...,"output":[...],"usage":{...}}}。
	// 只在顶层找 usage / output 会同时丢掉两样东西 —— 用量永远「未上报」，
	// 以及**所有工具调用**（模型给的 function_call 就在 output 里）。
	// 后者不报错、不告警，症状只是模型说了一句「正在拉取…」就没有下文：
	// 会话层看到的是「这一轮没有任何工具调用」，于是直接收尾。
	payload := root
	if sub, ok := root["response"].(map[string]any); ok {
		payload = sub
	}
	if raw, ok := payload["usage"]; ok && raw != nil {
		if u := usageFromJSON(raw); u.Reported {
			acc.usage = u
		}
	}

	// fillerText / fillerThink 记录本次 completed 事件补进来的内容：
	// 事件本身要把它一并推给调用方，否则界面上是「接口返回了正文、
	// 前端直到结束都没显示过它」（只在校准那一步可见，观感是突然冒出来）。
	var fillerText, fillerThink string

	switch event {
	case responsesEventCompleted:
		// 工具调用只在 completed 的负载里解析一次：output[] 是完整的一份，
		// 顺手处理 output_item.done 反而会与这里重复收集（同一个 call 进两次，
		// 结果是模型收到两份一样的工具结果）。
		if output, ok := payload["output"].([]any); ok {
			for i, c := range parseResponsesToolCalls(output) {
				acc.mergeCallPart(i, c.ID, c.Name, c.Arguments)
			}
			// 这一族在流式下**并非一定**发 output_text.delta 事件 ——
			// 实测遇到过一次「只有 completed、正文全在 output[] 里」：只认累积器的
			// 实现会拿到空回答，用户看到「这次没能拿到回答」而模型其实答了。
			// 只在累积器空的时候补，避免与 delta 拼出来的那份重复。
			if acc.text.Len() == 0 {
				if full := strings.TrimSpace(parseResponsesOutputText(output)); full != "" {
					acc.text.WriteString(full)
					fillerText = full
				}
			}
			if acc.reasoning.Len() == 0 {
				if think := strings.TrimSpace(parseResponsesReasoning(output)); think != "" {
					acc.reasoning.WriteString(think)
					fillerThink = think
				}
			}
		}
		acc.done = true
		return StreamDelta{Text: fillerText, Reasoning: fillerThink, Done: true, Usage: acc.usage}, true
	case responsesEventTextDelta:
		text, _ := root["delta"].(string)
		if text == "" {
			text, _ = root["text"].(string)
		}
		acc.text.WriteString(text)
		return StreamDelta{Text: text}, false
	case responsesEventReasoningDelta:
		text, _ := root["delta"].(string)
		if text == "" {
			text, _ = root["text"].(string)
		}
		acc.reasoning.WriteString(text)
		return StreamDelta{Reasoning: text}, false
	}
	return StreamDelta{}, false
}
