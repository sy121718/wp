package aiservice

// ai_chat_stream.go — 出站层的流式调用。
//
// 与非流式的关系：**同一个 buildProtocolRequest、同一份流水记账、同一个 ProtocolReply 出口**，
// 差别只在「怎么读响应体」与「读完怎么汇合」。刻意不复制非流式那条路径：
// 复制出来的分支会在下次改地址拼接 / SSRF 校验 / 密钥解密 / 自定义头时被漏掉，
// 而漏掉的症状是流式下这些保护静默失效。
//
// 为什么值得做：一次要跑几个工具的任务里，正文可能几十秒不出字。非流式下用户
// 那段时间只看到一个没反应的按钮，只能猜是不是卡死了。

import (
	"bufio"
	"context"
	"io"

	"strings"
	"time"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aimodel "go_wp/internal/module/ai/model"
	"go_wp/pkg/logger"
)

// streamScanBufMax 单行 SSE 的最大字节数。
//
// bufio.Scanner 的默认上限是 64KB，而**一个 delta 行可以很长**（长正文片、或
// 一次性吐出的大段工具参数）。超限时 Scanner 直接报 bufio.ErrTooLong 并停止 ——
// 表现为「回答到一半就断了、并且没有任何错误提示」，很难联想到行太长。
const streamScanBufMax = 1 << 20

// ChatStream 走 SSE 流式调用，逐片回调 onDelta；返回与非流式同形的结果。
//
// onDelta 为 nil 时等价于「只要最终结果」（退化成非流式语义，但仍走流式通道）。
// 回调在**调用者的 goroutine**里同步执行：它必须足够快且不能阻塞 ——
// 写响应流是它唯一该做的事，任何耗时操作都会让上游的读循环停住。
//
// 错误语义与非流式一致（同一批哨兵错误），调用方不必区分两条路径。
func (s *Service) ChatStream(ctx context.Context, req *aidto.ChatReq, onDelta func(StreamDelta)) (*aidto.ChatResult, error) {
	if req == nil {
		return nil, ErrInvalidParam
	}
	providerKey := strings.TrimSpace(req.ProviderKey)
	modelName := strings.TrimSpace(req.Model)
	if providerKey == "" || modelName == "" {
		return nil, ErrInvalidParam
	}

	// 流水与非流式共用同一套记账（含失败也落一条）。
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
	if rerr := readStreamBody(resp.Body, provider.Protocol, acc, onDelta); rerr != nil {
		// **已经产出的部分不丢**：错误往上抛，但调用方手里仍有已回调出去的正文。
		// 这里不把累积结果塞进返回值 —— 那会让「失败的调用」看起来有结果。
		log.Error(rerr, "流式对话失败：读取响应中断")
		return fail(ErrInternal)
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

// readStreamBody 按协议消费 SSE 正文，逐片回调。
//
// 两族协议的承载不同：chat 只有 `data:` 行，responses 还要看 `event:` 行
// （同一片 data 在 completed 事件里与在 delta 事件里意思完全不同）。
// 空行是事件分隔符、`:` 开头是注释（网关心跳），都跳过。
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
			// 注释行（SSE 的心跳）。不重置 event：它出现在事件中间时不表示事件结束。
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
	// 流**没有结束标记**就断了（上游掐断、或网关把非流式响应回给了我们）。
	// 累出来的东西仍然有效，所以只在上游压根没给过任何内容时才算失败 ——
	// 把「有半截回答的流」判成失败会让用户已经看到的字凭空消失。
	if !acc.done && acc.text.Len() == 0 && acc.reasoning.Len() == 0 && len(acc.calls) == 0 {
		return ErrInternal
	}
	return nil
}
