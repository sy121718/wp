package aiservice

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"io"
	"strings"
	"time"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aimodel "go_wp/internal/module/ai/model"
	"go_wp/pkg/logger"
)

const defaultChatMaxOutputTokens = 4096

func (s *Service) Chat(ctx context.Context, req *aidto.ChatReq) (res *aidto.ChatResult, err error) {
	if req == nil {
		return nil, ErrInvalidParam
	}
	providerKey := strings.TrimSpace(req.ProviderKey)
	model := strings.TrimSpace(req.Model)
	if providerKey == "" || model == "" {
		return nil, ErrInvalidParam
	}

	entry := &aimodel.AICallLogEntity{
		SessionID:   req.SessionID,
		UserID:      req.UserID,
		ProviderKey: providerKey,
		ModelID:     model,
	}
	started := time.Now()
	defer func() {
		entry.LatencyMs = time.Since(started).Milliseconds()
		if err != nil {
			entry.Status = string(aienums.CallStatusError)
			entry.ErrorKey = callErrorKey(err)
		} else {
			entry.Status = string(aienums.CallStatusOK)
		}
		s.logCallAsync(ctx, entry)
	}()

	provider, ferr := s.m.FindByKey(ctx, providerKey)
	if ferr != nil {
		return nil, ferr
	}
	if provider == nil || normalizeStatus(provider.Status) != aienums.StatusEnabled {
		return nil, ErrProviderNotFound
	}
	entry.Protocol = provider.Protocol

	maxTokens := req.MaxOutputTokens
	if maxTokens <= 0 {
		maxTokens = defaultChatMaxOutputTokens
		if cap := catalogMaxOutputTokens(provider, model); cap > 0 && cap < maxTokens {
			maxTokens = cap
		}
	}

	reqCtx, cancel := context.WithTimeout(ctx, ClientTimeout)
	defer cancel()

	msgs := req.Messages
	if len(msgs) == 0 {
		msgs = []aidto.ChatMessage{{Role: roleUser, Content: req.Input}}
	}
	httpReq, berr := s.buildProtocolRequest(reqCtx, provider, protocolRequestInput{
		Model:           model,
		Messages:        msgs,
		Tools:           req.Tools,
		MaxOutputTokens: maxTokens,
	})
	if berr != nil {
		return nil, berr
	}
	log := logger.Scene("ai").With("provider", provider.ProviderKey).With("model", model)
	resp, derr := s.client.Do(httpReq)
	if derr != nil {
		log.Error(derr, "对话失败：请求未发出")
		return nil, ErrInternal
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.With("status", resp.StatusCode).Error(ErrInternal, "对话失败：上游非 2xx")
		return nil, ErrInternal
	}
	body, rerr := io.ReadAll(io.LimitReader(resp.Body, MaxModelsRead))
	if rerr != nil {
		log.Error(rerr, "对话失败：读取响应中断")
		return nil, ErrInternal
	}
	reply, perr := parseProtocolReply(provider.Protocol, body)
	if perr != nil {
		return nil, perr
	}
	usage := reply.Usage

	entry.InputTokens = usage.InputTokens
	entry.OutputTokens = usage.OutputTokens
	entry.TotalTokens = usage.TotalTokens
	entry.CachedTokens = usage.CachedTokens
	entry.CachedReported = usage.CachedReported
	entry.UsageReported = usage.Reported

	return &aidto.ChatResult{
		ProviderKey:   provider.ProviderKey,
		Model:         model,
		Output:        reply.Content,
		Reasoning:     reply.Reasoning,
		ToolCalls:     reply.ToolCalls,
		Protocol:      provider.Protocol,
		InputTokens:   usage.InputTokens,
		OutputTokens:  usage.OutputTokens,
		TotalTokens:   usage.TotalTokens,
		CachedTokens:  usage.CachedTokens,
		UsageReported: usage.Reported,
	}, nil
}

func catalogMaxOutputTokens(provider *aimodel.AIProviderEntity, model string) int64 {
	for _, m := range parseModels(provider.ConfigData) {
		if m.ID == model {
			return m.MaxOutputTokens
		}
	}
	if builtin, ok := BuiltinModels(provider.ProviderKey); ok {
		for _, m := range builtin {
			if m.ID == model {
				return m.MaxOutputTokens
			}
		}
	}
	return 0
}

const streamScanBufMax = 1 << 20

const streamRawCap = 2 << 20

func headSample(body []byte) string {
	const cap = 240
	s := strings.TrimSpace(string(body))
	if idx := strings.IndexAny(s, "\r\n"); idx >= 0 && idx < cap {
		s = s[:idx]
	}
	if len(s) > cap {
		s = s[:cap] + "…"
	}
	return s
}

func rescueNonStream(body []byte, protocol string) (ProtocolReply, bool) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return ProtocolReply{}, false
	}
	payload := trimmed
	if !json.Valid(payload) {

		var sb strings.Builder
		for _, line := range strings.Split(string(trimmed), "\n") {
			line = strings.TrimRight(line, "\r")
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			piece := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if piece == "" || piece == streamDoneMarker {
				continue
			}
			sb.WriteString(piece)
		}
		payload = []byte(sb.String())
		if len(payload) == 0 || !json.Valid(payload) {
			return ProtocolReply{}, false
		}
	}
	var reply ProtocolReply
	var err error
	if protocol == aienums.ProtocolOpenAIResponses {
		reply, err = parseResponsesReply(payload)
	} else {
		reply, err = parseChatCompletionsReply(payload)
	}
	if err != nil {
		return ProtocolReply{}, false
	}
	return reply, true
}

func (s *Service) ChatStream(ctx context.Context, req *aidto.ChatReq, onDelta func(StreamDelta)) (*aidto.ChatResult, error) {
	if req == nil {
		return nil, ErrInvalidParam
	}
	providerKey := strings.TrimSpace(req.ProviderKey)
	modelName := strings.TrimSpace(req.Model)
	if providerKey == "" || modelName == "" {
		return nil, ErrInvalidParam
	}

	entry := &aimodel.AICallLogEntity{
		SessionID:   req.SessionID,
		UserID:      req.UserID,
		ProviderKey: providerKey,
		ModelID:     modelName,
	}
	started := time.Now()
	var streamErr error
	defer func() {
		entry.LatencyMs = time.Since(started).Milliseconds()
		if streamErr != nil {
			entry.Status = string(aienums.CallStatusError)
			entry.ErrorKey = callErrorKey(streamErr)
		} else {
			entry.Status = string(aienums.CallStatusOK)
		}
		s.logCallAsync(ctx, entry)
	}()

	fail := func(err error) (*aidto.ChatResult, error) {
		streamErr = err
		return nil, err
	}

	provider, ferr := s.m.FindByKey(ctx, providerKey)
	if ferr != nil {
		return fail(ferr)
	}
	if provider == nil || normalizeStatus(provider.Status) != aienums.StatusEnabled {
		return fail(ErrProviderNotFound)
	}
	entry.Protocol = provider.Protocol

	maxTokens := req.MaxOutputTokens
	if maxTokens <= 0 {
		maxTokens = defaultChatMaxOutputTokens
		if cap := catalogMaxOutputTokens(provider, modelName); cap > 0 && cap < maxTokens {
			maxTokens = cap
		}
	}

	reqCtx, cancel := context.WithTimeout(ctx, ClientTimeout)
	defer cancel()
	msgs := req.Messages
	if len(msgs) == 0 {
		msgs = []aidto.ChatMessage{{Role: roleUser, Content: req.Input}}
	}
	httpReq, berr := s.buildProtocolRequest(reqCtx, provider, protocolRequestInput{
		Model:           modelName,
		Messages:        msgs,
		Tools:           req.Tools,
		MaxOutputTokens: maxTokens,
		Stream:          true,
	})
	if berr != nil {
		return fail(berr)
	}
	log := logger.Scene("ai").With("provider", provider.ProviderKey).With("model", modelName)
	resp, derr := s.client.Do(httpReq)
	if derr != nil {
		log.Error(derr, "流式对话失败：请求未发出")
		return fail(ErrInternal)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.With("status", resp.StatusCode).Error(ErrInternal, "流式对话失败：上游非 2xx")
		return fail(ErrInternal)
	}

	acc := newStreamAccumulator()

	var raw bytes.Buffer
	body := io.TeeReader(io.LimitReader(resp.Body, streamRawCap), &raw)
	if rerr := readStreamBody(body, provider.Protocol, acc, onDelta); rerr != nil {

		log.Error(rerr, "流式对话失败：读取响应中断")
		return fail(ErrInternal)
	}
	if acc.isSilent() {

		if rescued, ok := rescueNonStream(raw.Bytes(), provider.Protocol); ok {
			log.With("bytes", raw.Len()).Error(ErrInternal, "流式对话失败：上游回了非流式响应，已按非流式解析")
			acc.adoptProtocolReply(rescued)
			onDelta(StreamDelta{Text: rescued.Content, Reasoning: rescued.Reasoning})
		} else {
			log.With("bytes", raw.Len()).
				With("contentType", resp.Header.Get("Content-Type")).
				With("head", headSample(raw.Bytes())).
				Error(ErrInternal, "流式对话失败：整轮没有任何可识别的事件")
			return fail(ErrInternal)
		}
	}

	reply := acc.Reply()
	usage := reply.Usage
	entry.InputTokens = usage.InputTokens
	entry.OutputTokens = usage.OutputTokens
	entry.TotalTokens = usage.TotalTokens
	entry.CachedTokens = usage.CachedTokens
	entry.CachedReported = usage.CachedReported
	entry.UsageReported = usage.Reported

	return &aidto.ChatResult{
		ProviderKey:   provider.ProviderKey,
		Model:         modelName,
		Output:        reply.Content,
		Reasoning:     reply.Reasoning,
		ToolCalls:     reply.ToolCalls,
		Protocol:      provider.Protocol,
		InputTokens:   usage.InputTokens,
		OutputTokens:  usage.OutputTokens,
		TotalTokens:   usage.TotalTokens,
		CachedTokens:  usage.CachedTokens,
		UsageReported: usage.Reported,
	}, nil
}

func readStreamBody(body io.Reader, protocol string, acc *streamAccumulator, onDelta func(StreamDelta)) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), streamScanBufMax)

	event := ""
	emit := func(d StreamDelta) {
		if onDelta == nil {
			return
		}
		if d.Text == "" && d.Reasoning == "" && !d.Done {
			return
		}
		onDelta(d)
	}
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		switch {
		case line == "":
			event = ""
		case strings.HasPrefix(line, ":"):

		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			payload := []byte(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			var delta StreamDelta
			var done bool
			if protocol == aienums.ProtocolOpenAIResponses {
				delta, done = feedResponsesEvent(event, payload, acc)
			} else {
				delta, done = feedChatChunk(payload, acc)
			}
			if done {
				emit(delta)
				return nil
			}
			emit(delta)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	if !acc.done && acc.text.Len() == 0 && acc.reasoning.Len() == 0 && len(acc.calls) == 0 {
		return ErrInternal
	}
	return nil
}

const callLogTimeout = 5 * time.Second

type CallLogWriter interface {
	Insert(ctx context.Context, e *aimodel.AICallLogEntity) error
}

func (s *Service) SetCallLogWriter(w CallLogWriter) { s.calls = w }

func (s *Service) logCallAsync(ctx context.Context, e *aimodel.AICallLogEntity) {
	if s == nil || s.calls == nil || e == nil {
		return
	}

	if ctx == nil {
		ctx = context.Background()
	}
	base := context.WithoutCancel(ctx)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Scene("ai").With("session", e.SessionID).With("panic", r).
					Error(nil, "AI 调用流水写入协程发生 panic，已忽略")
			}
		}()
		wctx, cancel := context.WithTimeout(base, callLogTimeout)
		defer cancel()
		if err := s.calls.Insert(wctx, e); err != nil {
			logger.Scene("ai").
				With("session", e.SessionID).
				With("provider", e.ProviderKey).
				With("model", e.ModelID).
				Error(err, "AI 调用流水写入失败（只记日志，不影响本次调用结果）")
		}
	}()
}

var callErrorSentinels = []error{
	ErrProviderNotFound,
	ErrInvalidParam,
	ErrProtocolUnsupported,
	ErrBaseURLRequired,
	ErrCipherUnavailable,
	ErrModelsFetchFailed,
	ErrURLMalformed,
	ErrURLSchemeUnsupported,
	ErrURLHostMissing,
	ErrURLUnresolvable,
	ErrURLDenied,
	ErrInternal,
}

func callErrorKey(err error) string {
	for _, sentinel := range callErrorSentinels {
		if errors.Is(err, sentinel) {
			return sentinel.Error()
		}
	}
	return aienums.ErrInternal
}

func chatMessageContent(m aidto.ChatMessage) any {
	if len(m.Images) == 0 {
		return m.Content
	}
	parts := make([]map[string]any, 0, len(m.Images)+1)
	if text := strings.TrimSpace(m.Content); text != "" {
		parts = append(parts, map[string]any{"type": "text", "text": m.Content})
	}
	for _, url := range m.Images {
		parts = append(parts, map[string]any{
			"type":      "image_url",
			"image_url": map[string]any{"url": url},
		})
	}
	return parts
}

func buildChatCompletionsBody(model string, msgs []aidto.ChatMessage, tools []aidto.ToolSpec, maxOutputTokens int64, stream bool) ([]byte, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, ErrInvalidParam
	}
	if len(msgs) == 0 {
		return nil, ErrInvalidParam
	}
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		item := map[string]any{"role": m.Role, "content": chatMessageContent(m)}
		if len(m.ToolCalls) > 0 {
			item["tool_calls"] = chatToolCalls(m.ToolCalls)
		}
		if m.ToolCallID != "" {
			item["tool_call_id"] = m.ToolCallID
		}
		if m.Name != "" {
			item["name"] = m.Name
		}
		out = append(out, item)
	}
	payload := map[string]any{"model": model, "messages": out}
	if stream {
		payload["stream"] = true

		payload["stream_options"] = map[string]any{"include_usage": true}
	}
	if len(tools) > 0 {
		payload["tools"] = chatTools(tools)
		payload["tool_choice"] = toolChoiceAuto
	}
	if maxOutputTokens > 0 {
		payload["max_tokens"] = maxOutputTokens
	}
	return json.Marshal(payload)
}

func chatTools(tools []aidto.ToolSpec) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		fn := map[string]any{"name": t.Name, "description": t.Description}
		if len(t.Parameters) > 0 {
			fn["parameters"] = json.RawMessage(t.Parameters)
		} else {
			fn["parameters"] = emptyObjectSchema()
		}
		out = append(out, map[string]any{"type": toolTypeFunction, "function": fn})
	}
	return out
}

func chatToolCalls(calls []aidto.ToolCall) []map[string]any {
	out := make([]map[string]any, 0, len(calls))
	for _, c := range calls {
		out = append(out, map[string]any{
			"id":   c.ID,
			"type": toolTypeFunction,
			"function": map[string]any{
				"name": c.Name,

				"arguments": orEmptyJSONObject(c.Arguments),
			},
		})
	}
	return out
}

func parseChatCompletionsReply(body []byte) (ProtocolReply, error) {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return ProtocolReply{}, ErrInternal
	}
	usage := usageFromJSON(root["usage"])
	choices, _ := root["choices"].([]any)
	if len(choices) == 0 {
		return ProtocolReply{Usage: usage}, ErrInternal
	}
	first, _ := choices[0].(map[string]any)
	msg, _ := first["message"].(map[string]any)
	text, _ := msg["content"].(string)
	calls := parseChatToolCalls(msg["tool_calls"])

	reasoning := firstNonEmptyString(msg, "reasoning_content", "reasoning")

	if strings.TrimSpace(text) == "" && len(calls) == 0 {
		return ProtocolReply{Usage: usage}, ErrInternal
	}
	return ProtocolReply{Content: text, Reasoning: reasoning, ToolCalls: calls, Usage: usage}, nil
}

func firstNonEmptyString(obj map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := obj[k].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func parseChatToolCalls(raw any) []aidto.ToolCall {
	list, _ := raw.([]any)
	if len(list) == 0 {
		return nil
	}
	out := make([]aidto.ToolCall, 0, len(list))
	for _, item := range list {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		fn, _ := obj["function"].(map[string]any)
		name, _ := fn["name"].(string)
		if strings.TrimSpace(name) == "" {
			continue
		}
		args, _ := fn["arguments"].(string)
		id, _ := obj["id"].(string)
		out = append(out, aidto.ToolCall{ID: id, Name: name, Arguments: args})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

const (
	roleSystem    = "system"
	roleUser      = "user"
	roleAssistant = "assistant"
	roleTool      = "tool"
)

const (
	toolTypeFunction = "function"

	toolChoiceAuto = "auto"
)

type ProtocolReply struct {
	Content string

	Reasoning string

	ToolCalls []aidto.ToolCall

	Usage ReplyUsage
}

func emptyObjectSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func orEmptyJSONObject(args string) string {
	if strings.TrimSpace(args) == "" {
		return "{}"
	}
	return args
}

const (
	streamDoneMarker = "[DONE]"

	responsesEventCompleted      = "response.completed"
	responsesEventTextDelta      = "response.output_text.delta"
	responsesEventReasoningDelta = "response.reasoning_summary_text.delta"
)

const (
	responsesObjectName   = "response"
	responsesTextPartType = "output_text"

	responsesReasoningPartType = "reasoning"

	responsesCallPartType = "function_call"

	responsesCallOutputType = "function_call_output"

	responsesMessagePartType = "message"

	responsesStatusFailed = "failed"
)

func buildResponsesBody(model string, msgs []aidto.ChatMessage, tools []aidto.ToolSpec, maxOutputTokens int64, stream bool) ([]byte, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, ErrInvalidParam
	}
	if len(msgs) == 0 {
		return nil, ErrInvalidParam
	}
	instructions, rest := splitSystemMessages(msgs)

	if len(rest) == 0 {
		return nil, ErrInvalidParam
	}
	payload := map[string]any{"model": model, "input": responsesInput(rest)}
	if stream {

		payload["stream"] = true
	}
	if instructions != "" {
		payload["instructions"] = instructions
	}
	if len(tools) > 0 {
		payload["tools"] = responsesTools(tools)
		payload["tool_choice"] = toolChoiceAuto
	}
	if maxOutputTokens > 0 {
		payload["max_output_tokens"] = maxOutputTokens
	}
	return json.Marshal(payload)
}

func splitSystemMessages(msgs []aidto.ChatMessage) (string, []aidto.ChatMessage) {
	var b strings.Builder
	rest := make([]aidto.ChatMessage, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == roleSystem {

			if m.Content != "" {
				if b.Len() > 0 {
					b.WriteString("\n\n")
				}
				b.WriteString(m.Content)
			}
			continue
		}
		rest = append(rest, m)
	}
	return b.String(), rest
}

func responsesInput(msgs []aidto.ChatMessage) any {
	if len(msgs) == 1 && isPlainMessage(msgs[0]) {
		return msgs[0].Content
	}
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		if len(m.ToolCalls) > 0 {

			if text := strings.TrimSpace(m.Content); text != "" {
				out = append(out, map[string]any{"role": roleAssistant, "content": text})
			}
			for _, c := range m.ToolCalls {
				out = append(out, map[string]any{
					"type":      responsesCallPartType,
					"call_id":   c.ID,
					"name":      c.Name,
					"arguments": orEmptyJSONObject(c.Arguments),
				})
			}
			continue
		}
		if m.ToolCallID != "" {
			out = append(out, map[string]any{
				"type":    responsesCallOutputType,
				"call_id": m.ToolCallID,
				"output":  m.Content,
			})
			continue
		}
		out = append(out, map[string]any{"role": m.Role, "content": responsesMessageContent(m)})
	}
	return out
}

func responsesMessageContent(m aidto.ChatMessage) any {
	if len(m.Images) == 0 {
		return m.Content
	}
	parts := make([]map[string]any, 0, len(m.Images)+1)
	if text := strings.TrimSpace(m.Content); text != "" {
		parts = append(parts, map[string]any{"type": "input_text", "text": m.Content})
	}
	for _, url := range m.Images {
		parts = append(parts, map[string]any{"type": "input_image", "image_url": url})
	}
	return parts
}

func isPlainMessage(m aidto.ChatMessage) bool {

	return m.Role == roleUser && m.ToolCallID == "" && len(m.ToolCalls) == 0 && m.Name == "" && len(m.Images) == 0
}

func responsesTools(tools []aidto.ToolSpec) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		item := map[string]any{"type": toolTypeFunction, "name": t.Name, "description": t.Description}
		if len(t.Parameters) > 0 {
			item["parameters"] = json.RawMessage(t.Parameters)
		} else {
			item["parameters"] = emptyObjectSchema()
		}
		out = append(out, item)
	}
	return out
}

func parseResponsesReasoning(output []any) string {
	var parts []string
	for _, item := range output {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if kind, _ := obj["type"].(string); kind != responsesReasoningPartType {
			continue
		}
		for _, key := range []string{"summary", "content"} {
			for _, seg := range asAnyList(obj[key]) {
				if m, ok := seg.(map[string]any); ok {
					parts = append(parts, firstNonEmptyString(m, "text", "summary_text"))
				} else if s, ok := seg.(string); ok && strings.TrimSpace(s) != "" {
					parts = append(parts, s)
				}
			}
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func asAnyList(v any) []any {
	if list, ok := v.([]any); ok {
		return list
	}
	return nil
}

func parseResponsesReply(body []byte) (ProtocolReply, error) {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return ProtocolReply{}, ErrInternal
	}
	if obj, _ := root["object"].(string); obj != "" && obj != responsesObjectName {
		return ProtocolReply{}, ErrInternal
	}
	if status, _ := root["status"].(string); status == responsesStatusFailed {
		return ProtocolReply{}, ErrInternal
	}

	usage := usageFromJSON(root["usage"])

	output, _ := root["output"].([]any)
	calls := parseResponsesToolCalls(output)
	reasoning := parseResponsesReasoning(output)

	if text, ok := root["output_text"].(string); ok && text != "" {
		return ProtocolReply{Content: text, Reasoning: reasoning, ToolCalls: calls, Usage: usage}, nil
	}

	text := parseResponsesOutputText(output)
	if strings.TrimSpace(text) == "" && len(calls) == 0 {
		return ProtocolReply{Usage: usage}, ErrInternal
	}
	return ProtocolReply{Content: text, Reasoning: reasoning, ToolCalls: calls, Usage: usage}, nil
}

func parseResponsesOutputText(output []any) string {
	var sb strings.Builder
	for _, item := range output {
		msg, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if kind, _ := msg["type"].(string); kind != "" && kind != responsesMessagePartType {
			continue
		}
		content, _ := msg["content"].([]any)
		for _, part := range content {
			piece, ok := part.(map[string]any)
			if !ok {
				continue
			}
			if kind, _ := piece["type"].(string); kind != responsesTextPartType {
				continue
			}
			if text, _ := piece["text"].(string); text != "" {
				if sb.Len() > 0 {
					sb.WriteString("\n")
				}
				sb.WriteString(text)
			}
		}
	}
	return sb.String()
}

func parseResponsesToolCalls(output []any) []aidto.ToolCall {
	var out []aidto.ToolCall
	for _, item := range output {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if kind, _ := obj["type"].(string); kind != responsesCallPartType {
			continue
		}
		name, _ := obj["name"].(string)
		if strings.TrimSpace(name) == "" {
			continue
		}
		args, _ := obj["arguments"].(string)
		id, _ := obj["call_id"].(string)
		out = append(out, aidto.ToolCall{ID: id, Name: name, Arguments: args})
	}
	return out
}

type StreamDelta struct {
	Text string

	Reasoning string

	Done bool

	Usage ReplyUsage
}

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

func (a *streamAccumulator) Reply() ProtocolReply {
	calls := make([]aidto.ToolCall, 0, len(a.order))
	for _, idx := range a.order {
		call := a.calls[idx]
		if call == nil || strings.TrimSpace(call.Name) == "" {

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

func (a *streamAccumulator) isSilent() bool {
	return !a.done && a.text.Len() == 0 && a.reasoning.Len() == 0 && len(a.calls) == 0
}

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

		return StreamDelta{}, false
	}

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

	payload := root
	if sub, ok := root["response"].(map[string]any); ok {
		payload = sub
	}
	if raw, ok := payload["usage"]; ok && raw != nil {
		if u := usageFromJSON(raw); u.Reported {
			acc.usage = u
		}
	}

	var fillerText, fillerThink string

	switch event {
	case responsesEventCompleted:

		if output, ok := payload["output"].([]any); ok {
			for i, c := range parseResponsesToolCalls(output) {
				acc.mergeCallPart(i, c.ID, c.Name, c.Arguments)
			}

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
