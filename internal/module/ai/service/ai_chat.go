package aiservice

// 复用面（ai_client.go 的协议层，本文件不重复实现协议）：
//
//	buildProtocolRequest(ctx, provider, model, input, maxOutputTokens) → 组装一次出站请求，
//	  内部走 applyProviderHeaders(config_data.headers) 与 Authorization: Bearer <明文密钥>；
//	parseProtocolReply(protocol, body) → 按协议解析出文本与用量（未实现的协议回 ErrProtocolUnsupported）。
//
// 密钥口径（与 ai_service.go 头注释一致）：明文只在 buildProtocolRequest 内部解密后进请求头，
// 本文件不接触明文，返回值也不含密钥。

// 与非流式的关系：**同一个 buildProtocolRequest、同一份流水记账、同一个 ProtocolReply 出口**，
// 差别只在「怎么读响应体」与「读完怎么汇合」。刻意不复制非流式那条路径：
// 复制出来的分支会在下次改地址拼接 / SSRF 校验 / 密钥解密 / 自定义头时被漏掉，
// 而漏掉的症状是流式下这些保护静默失效。
//
// 为什么值得做：一次要跑几个工具的任务里，正文可能几十秒不出字。非流式下用户
// 那段时间只看到一个没反应的按钮，只能猜是不是卡死了。

// 落点是**唯一出站点**（Service.Chat），不是某个调用方：这样无论谁发起对话
// （会话页发消息、对外 JSON 接口、将来的批量任务），流水都记得到，不需要每个调用方各写一遍。

// 与 responses 的镜像关系：请求体是 {model, messages:[...], tools?, max_tokens?}，正文取
// choices[0].message.content，工具调用取 choices[0].message.tool_calls。两者放在同一层，
// 由 ai_client.go 的分派函数二选一。

// 放在这里而不是任一份协议实现里：两边都要用，谁先写谁拥有的结果会是
// 「改一个常量要先去另一份文件里找」。

// 与 chat/completions 的差异（这也是 muse 系列只吃它的原因）：
//
//	· 请求体是 {model, input, max_output_tokens}，input 可以是字符串（单条消息）或消息数组；
//	· 工具往返**不是 message**：调用是 output[] 里 type=function_call 的独立条目，
//	  结果是 input[] 里 type=function_call_output 的独立条目（靠 call_id 配对）；
//	· 响应是 {"object":"response", ...}，正文藏在 output[].content[]（type=output_text）里，
//	  顶层没有 chat/completions 的 choices[0].message.content。
//
// 只做「构造 / 解析」这一层纯函数，不碰 DB、不发请求 —— 出站由 ai_client.go 的分派装配，
// 这样单测可以直接喂 JSON，不必起 HTTP 服务。

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
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"go_wp/internal/module/ai/dto"
	"go_wp/internal/module/ai/enums"
	"go_wp/internal/module/ai/model"
	"go_wp/pkg/logger"
)

// defaultChatMaxOutputTokens 目录里查不到该模型的 max_output_tokens 时的默认上限。
const defaultChatMaxOutputTokens = 4096

// Chat 一次最小对话：一个 provider_key + model 打一次上游，拿回文本。
//
// 「供应商不存在」与「供应商已停用」同口径归口 ErrProviderNotFound —— 不向调用方区分
// 「没配」与「配了但停用」，不把供应商的存在性变成可探测的信息。
// 上游非 2xx / 网络失败一律归口 ErrInternal（enums 的 key，不是中文原文）：上游报文原文
// **只进日志**，不进用户可见错误（可能含密钥回显与内部标识）。
func (s *Service) Chat(ctx context.Context, req *aidto.ChatReq) (res *aidto.ChatResult, err error) {
	if req == nil {
		return nil, ErrInvalidParam
	}
	providerKey := strings.TrimSpace(req.ProviderKey)
	model := strings.TrimSpace(req.Model)
	if providerKey == "" || model == "" {
		return nil, ErrInvalidParam
	}

	// 调用流水从这一行开始记账：**成功与失败都要落一条** —— 只记成功的日志在排查故障时等于没有
	// （「用户说模型报错了」时最需要的恰恰是失败那条）。用 defer 收口是为了不遗漏任何一个 return。
	//
	// 归属（session_id / user_id）来自请求，但由服务端填（见 aidto.ChatReq 的注释）；
	// provider/model 记 trim 后的**实际出站值**，protocol 在取到供应商之后再补。
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

	// 上限：调用方给了就用它；没给按默认值，但不超过目录里该模型的 max_output_tokens
	// （目录查不到就用默认值）—— 目录里的 0 表示「未知」，不参与封顶。
	maxTokens := req.MaxOutputTokens
	if maxTokens <= 0 {
		maxTokens = defaultChatMaxOutputTokens
		if cap := catalogMaxOutputTokens(provider, model); cap > 0 && cap < maxTokens {
			maxTokens = cap
		}
	}

	reqCtx, cancel := context.WithTimeout(ctx, ClientTimeout)
	defer cancel()
	// 两个入口在这里汇合：带工具的轮次给 Messages，一问一答只给 Input。
	// 空消息序列**不静默补空串**：Input 是调用方必填字段，缺了在下面的出站构造里报 ErrInvalidParam。
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
	// 用量只记上游**上报**的值：这家没报就留 0 + UsageReported=false，
	// 不在这里估算补齐（估算值混进流水会被当成真用量）。
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

// catalogMaxOutputTokens 在供应商的模型目录（config_data.models）里找该 model 的
// max_output_tokens；目录里没有就退回代码内置清单；都查不到 / 值为未知（0）回 0。
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

// streamScanBufMax 单行 SSE 的最大字节数。
//
// bufio.Scanner 的默认上限是 64KB，而**一个 delta 行可以很长**（长正文片、或
// 一次性吐出的大段工具参数）。超限时 Scanner 直接报 bufio.ErrTooLong 并停止 ——
// 表现为「回答到一半就断了、并且没有任何错误提示」，很难联想到行太长。
const streamScanBufMax = 1 << 20

// streamRawCap 留样本的上限：只为「上游其实回了非流式」这一种情况兜底，
// 一份正常回答远小于它；超过就说明这不是我们要救的那类响应。
const streamRawCap = 2 << 20

// headSample 取一段可读的响应开头（写日志用）。
//
// 换行与超长都截掉：日志要的是一眼能看出「这是什么」，不是全文。
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

// rescueNonStream 试着把一段**没被 SSE 解析器认出任何事件**的响应体当非流式解析。
//
// 两种形态都接受：整段就是一份 JSON（网关直接把非流式结果回给我们），
// 以及「有 data: 前缀但只有一片」的 SSE（`data: {整份结果}`）。
// 回 false 时调用方应当按错误处理 —— 这里绝不猜，猜错会把「上游报错」变成「空回答」。
func rescueNonStream(body []byte, protocol string) (ProtocolReply, bool) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return ProtocolReply{}, false
	}
	payload := trimmed
	if !json.Valid(payload) {
		// 退一步：把全部 data: 行拼起来再看。
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
	// 边读边留一份原始字节：上游有时**不按流式回**（严格做 content negotiation 的
	// 网关会把整段回答一次性给出来），此时 SSE 解析器一个事件都认不出来 ——
	// 症状是「前面的工具调用都成功了，最后一轮却整轮失败」。留样本才可能把它救回来
	// （下面 rescueNonStream）以及把真相写进日志。
	var raw bytes.Buffer
	body := io.TeeReader(io.LimitReader(resp.Body, streamRawCap), &raw)
	if rerr := readStreamBody(body, provider.Protocol, acc, onDelta); rerr != nil {
		// **已经产出的部分不丢**：错误往上抛，但调用方手里仍有已回调出去的正文。
		// 这里不把累积结果塞进返回值 —— 那会让「失败的调用」看起来有结果。
		log.Error(rerr, "流式对话失败：读取响应中断")
		return fail(ErrInternal)
	}
	if acc.isSilent() {
		// 一整轮下来什么都没收到：要么上游没按流式回，要么它回了一个我们不认识的事件名。
		// 两种都先试着按非流式解一遍；解不出来才把样本写进日志（这条分支以前什么都不说，
		// 排查时连响应的样子都看不到）。
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

// callLogTimeout 单条调用流水的写入上限（独立于上游超时：上游 15s 之后还要留出落库时间）。
const callLogTimeout = 5 * time.Second

// CallLogWriter 调用流水的写入端口（由 model 实现，装配期注入）。
//
// 用接口而不是直接持 *aimodel.CallLogModel：出站层对这张表只有「追加一条」这一件事，
// 端口形状把能力收窄到这一件事上（用例也能换一个记录器进来断言写了什么）。
type CallLogWriter interface {
	Insert(ctx context.Context, e *aimodel.AICallLogEntity) error
}

// SetCallLogWriter 注入调用流水写入端口；未注入时 logCallAsync 是空操作（不 panic）。
func (s *Service) SetCallLogWriter(w CallLogWriter) { s.calls = w }

// logCallAsync 异步落一条调用流水。
//
// 三条「为什么必须这么写」（改回同步或改回原 ctx 都会踩）：
//
//  1. **旁路观测不该惩罚正常路径**：写日志的往返（含库抖动时的重连）会直接加在用户等回复的时间上。
//  2. **必须脱离请求 ctx**：HTTP 请求返回后 ctx 立刻被取消，用原 ctx 异步写等于 100% 写不进去 ——
//     这不是偶发竞态而是必然，所以走 context.WithoutCancel 再配自己的超时。
//  3. **panic 与错误都不许外溢**：这是纯观测路径，任何写失败都不能影响业务返回、更不能带走进程。
//
// 已知代价（写在这里免得后人当 bug 查）：进程在协程落库前退出，这条流水会丢。
// 观测数据丢一条不影响业务正确性，这是用「不阻塞用户」换来的，接受。
func (s *Service) logCallAsync(ctx context.Context, e *aimodel.AICallLogEntity) {
	if s == nil || s.calls == nil || e == nil {
		return
	}
	// context.WithoutCancel(nil) 会 panic，先兜住 nil（与 admin 的数据权限快照同口径）。
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

// callErrorSentinels 允许落进 ai_call_log.error_key 的哨兵。
//
// 它们的**值本身就是 i18n key**（enums 口径），所以可以直接入库；
// 不在这个集合里的错误一律归口 ErrInternal —— 底层原文（上游报文片段、SQL 片段）
// 只进日志，不进表：流水表会被后台页面读，原文透出去就是信息泄漏。
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

// callErrorKey 把一次失败归口成可入库的 key；识别不出（含被包装过的非哨兵错误）回 ErrInternal。
func callErrorKey(err error) string {
	for _, sentinel := range callErrorSentinels {
		if errors.Is(err, sentinel) {
			return sentinel.Error()
		}
	}
	return aienums.ErrInternal
}

// buildChatCompletionsBody 构造 /chat/completions 的请求体。
//
// messages 的每个元素按角色带不同的附加字段：assistant 带 tool_calls、tool 带 tool_call_id。
// content **始终写入**（哪怕是空串）：带 tool_calls 的 assistant 消息正文本来就是空的，
// 而部分网关要求 content 字段存在 —— 省掉它换来的是「400 缺少 content」这种与语义无关的故障。
//
// tools 非空时才写 tool_choice：显式写 auto 让「这轮允许调工具」在请求体里可见，
// 排查「模型为什么不调工具」时不必再去翻服务端的默认值。
//
// max_tokens 只在 > 0 时写入：0 会被服务端当成「最多生成 0 个 token」直接截断。
// chatMessageContent 把一条消息翻成 content 字段的取值：纯文本用字符串，带图用分片数组。
//
// 为什么不能一律用数组：纯文本消息用**字符串**是这条协议最稳的形态，某些网关对
// 纯文本数组的处理不如字符串（表现是「模型收到的正文变成 [object]」这类怪事）。
// 所以只在真的有图时才升格成数组。
//
// 两族的图片分片形状**不一样**（chat 的 image_url 是对象、responses 的 image_url 是字符串），
// 各写各的，不抽公共函数 —— 抽出来就只能靠一个 protocol 参数分叉，
// 那和两份独立实现一样容易错，却更难看出错在哪一边。
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
		// stream_options.include_usage：**不加它，流式响应里根本没有 usage 对象**，
		// 于是计量那三个数（命中率 / 压缩开销）在流式下全部退化成「未上报」。
		// 这不是可选优化，是流式与计量能同时成立的前提。
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

// chatTools 把工具声明翻成 chat/completions 的 tools 形状（每个工具外面包一层 type=function）。
//
// parameters 缺失时补一个空对象 schema：不补的话请求体里是 null，而 OpenAI 兼容网关对
// tools[].function.parameters 为 null 的处理并不一致（有的当空对象、有的直接 400）。
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

// chatToolCalls 把工具调用翻回请求体的 tool_calls 形状（回灌历史轮次时用）。
func chatToolCalls(calls []aidto.ToolCall) []map[string]any {
	out := make([]map[string]any, 0, len(calls))
	for _, c := range calls {
		out = append(out, map[string]any{
			"id":   c.ID,
			"type": toolTypeFunction,
			"function": map[string]any{
				"name": c.Name,
				// arguments 缺失时补 "{}"：回灌时上游多半会校验它是字符串，
				// 而空串不是合法 JSON 对象，会让这一轮直接失败。
				"arguments": orEmptyJSONObject(c.Arguments),
			},
		})
	}
	return out
}

// parseChatCompletionsReply 从 /chat/completions 的响应体里取出正文、工具调用与用量。
//
// 形状不对（解不出 JSON、没有 choices）归口 ErrInternal；**正文为空但有待执行的工具调用不算错** ——
// 模型要调工具时正文本来就是空的，把这种中间态判成「服务器内部错误」会让第一次工具调用就失败。
// 判错的判据是「两者都空」：既没有话要说、也没有事要做，才是真的没产出。
//
// usage 缺失不算错：它只影响调用流水的用量列（Reported=false），正文该回还是要回 ——
// 把「这家没报 usage」判成失败会让一次成功的对话看起来像挂了。
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
	// 思考过程两家字段名不同：DeepSeek 系用 reasoning_content，另一些用 reasoning。
	// 都试一遍 —— 只认一个时，换一家供应商这个功能就静默消失（页面上看不出区别，
	// 只是「正在思考」那一段永远不出现）。
	reasoning := firstNonEmptyString(msg, "reasoning_content", "reasoning")
	// 空判断**不含 reasoning**：只有思考过程、没有正文也没有工具调用，等于这轮没产出。
	// 把它当成功会让外层拿一个空回答去写会话事件。
	if strings.TrimSpace(text) == "" && len(calls) == 0 {
		return ProtocolReply{Usage: usage}, ErrInternal
	}
	return ProtocolReply{Content: text, Reasoning: reasoning, ToolCalls: calls, Usage: usage}, nil
}

// firstNonEmptyString 按顺序取第一个非空的字符串字段。
func firstNonEmptyString(obj map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := obj[k].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// parseChatToolCalls 解析 message.tool_calls。
//
// 没有工具名的条目**直接跳过**而不是造一条空调用：空名字的调用执行不了，
// 留在列表里只会让上层拿着一串「查不到这个工具」的错误去问模型，白烧一轮。
// 一个都没解出来时回 nil（与「本来就没有 tool_calls」等价）。
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

// 对话角色（两个协议共用同一套名字）。
//
// system 的来源是 ai/prompt 包的常驻规则（SiteRules），由会话层放在消息列表**最前**：
// 它与会话历史一起构成稳定前缀，同一会话里反复发消息时逐字节不变（docs/16 §3）。
// 曾经这里写着「规则拼进历史文本、不单独占 system 消息」，理由是怕规则变化作废缓存 ——
// 那条注释对应的实现从未存在（`buildChatMessages` 是个幽灵名字），而且理由本身是错位的：
// 规则改得**不频繁**，作废一次缓存可以接受；真正每轮都变的是历史与输入，它们本来就该在尾部。
const (
	roleSystem    = "system"
	roleUser      = "user"
	roleAssistant = "assistant"
	roleTool      = "tool"
)

// 工具相关的协议字面量（两家一致的部分）。
const (
	// toolTypeFunction 工具类型标记：chat/completions 在 tools[].type，responses 在
	// function_call 条目的 type。两家都用 "function"。
	toolTypeFunction = "function"
	// toolChoiceAuto 由模型自己决定这轮要不要调工具。
	//
	// 刻意不用 "required"（必须调一个）：那会让「今天有几单」这种本来就该直接回答的提问
	// 也被强行塞一次调用。也不设白名单 —— 工具集该由调用方裁剪（docs/17 D4），
	// 而不是在请求体里再写一遍同样的名单（两处名单必然分叉）。
	toolChoiceAuto = "auto"
)

// ProtocolReply 一次上游响应的解析结果（两个协议统一的出站形状）。
//
// Content 与 ToolCalls 的**组合语义**：只有 Content 是「回答完了」，
// 只有 ToolCalls 是「要做点事」，两者都有是「先说了句话、再要做事」，都空才是解析失败。
type ProtocolReply struct {
	// Content 模型说的话；要求调工具时通常为空。
	Content string
	// Reasoning 模型的思考过程（部分上游在正文之外单独返回，字段名两家不同：
	// reasoning_content / reasoning）。
	//
	// 它**不是模型说的话**，所以不进对话历史；它的用途只有一个 —— 展示给用户
	// 「它在想什么」。一次要跑工具的任务里正文可能几十秒不出字，那段时间用户
	// 只看到一个没反应的按钮，只能猜是不是卡死了。
	Reasoning string
	// ToolCalls 模型要求执行的工具调用（可能多个，按上游给的顺序执行）。
	ToolCalls []aidto.ToolCall
	// Usage 上游上报的用量（未上报时 Reported=false）。
	Usage ReplyUsage
}

// emptyObjectSchema 一个「无参数」的 JSON Schema。
//
// 不写 parameters 时补它：OpenAI 兼容网关对 tools[].function.parameters 为 null
// 的处理并不一致（有的当空对象、有的直接 400），补一个显式空对象能把差异抹平。
func emptyObjectSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

// orEmptyJSONObject 空 arguments 补成 "{}"。
//
// 回灌历史轮次时才需要：上游校验 arguments 是字符串，而空串不是合法 JSON 对象。
func orEmptyJSONObject(args string) string {
	if strings.TrimSpace(args) == "" {
		return "{}"
	}
	return args
}

// 流式（SSE）相关的常量。
//
// 两族协议的结束方式不同：chat 用一行**字面量** [DONE]（不是 JSON），
// responses 用 response.completed 事件。用常量而不是散落的字面量，
// 是因为它们各有两处消费者（解析与请求构造），拼错一处不会报错、
// 只会让流永远不结束（表现为「一直转圈」）。
const (
	streamDoneMarker = "[DONE]"

	// responses 族的事件名。
	responsesEventCompleted      = "response.completed"
	responsesEventTextDelta      = "response.output_text.delta"
	responsesEventReasoningDelta = "response.reasoning_summary_text.delta"
)

// responses 响应里各条目的类型标记。
const (
	responsesObjectName   = "response"
	responsesTextPartType = "output_text"
	// responsesReasoningPartType 思考过程条目（output[] 里独立一项）。
	//
	// 在这一族协议里思维链不是 message 的一个字段，而是与 message 并列的一种条目：
	// output: [{type:"reasoning", summary:[{type:"summary_text", text:"..."}]}, {type:"message", ...}]。
	// 只读 content 会漏掉整段思考（而它恰恰是跑工具那几十秒里唯一的进展信号）。
	responsesReasoningPartType = "reasoning"
	// responsesCallPartType 工具调用条目（output[] 里独立一项）。
	responsesCallPartType = "function_call"
	// responsesCallOutputType 工具结果条目（input[] 里独立一项）。
	responsesCallOutputType = "function_call_output"
	// responsesMessagePartType 普通消息条目。
	responsesMessagePartType = "message"
	// responsesStatusFailed 是服务端明确宣告「这轮失败」，据此直接归错。
	// 状态 incomplete（被 max_output_tokens 截断 / 被内容过滤中止）语义不同：它不代表
	// 没有正文，所以这里**不建常量、也不参与判错**（见 parseResponsesReply 的取值路径），
	// 免得后人顺手拿它当失败状态用。
	responsesStatusFailed = "failed"
)

// buildResponsesBody 构造 /responses 的请求体。
//
// input 的形状按消息序列自适应：
//   - 只有一条平凡消息（无工具调用、无结果）时写成**字符串**，与历史实现逐字节一致 ——
//     这条路径覆盖了现在所有的调用（一问一答），字节不变就保住了上游的前缀缓存（docs/16 §3）；
//   - 出现工具往返时写成数组，让 function_call / function_call_output 能与 message 并列。
//
// **system 消息不走 input**：本协议把它放在顶层 `instructions`。这不是风格选择 ——
// input[] 里的合法角色是 user / assistant（工具条目另算），塞一个 system 进去会被服务端
// 400 拒掉。这个坑此前不会暴露，因为在这条改动之前**根本不存在 system 消息**
// （常驻规则那一段是刚加进会话层的）—— 也就是说，加了规则却不同时改这里，
// 第一次请求就会失败，而错误信息只会说请求体不合法。
//
// max_output_tokens 只在 > 0 时写入 —— 服务端对缺省值有自己的默认，塞 0 会被当成
// 「最多生成 0 个 token」而立刻截断。
func buildResponsesBody(model string, msgs []aidto.ChatMessage, tools []aidto.ToolSpec, maxOutputTokens int64, stream bool) ([]byte, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, ErrInvalidParam
	}
	if len(msgs) == 0 {
		return nil, ErrInvalidParam
	}
	instructions, rest := splitSystemMessages(msgs)
	// 抽走 system 之后 input 不能为空：上游要求 input 存在。
	// （正常路径下总会剩一条 user；真为空时按参数错误打回，而不是发一个必然被拒的请求体。）
	if len(rest) == 0 {
		return nil, ErrInvalidParam
	}
	payload := map[string]any{"model": model, "input": responsesInput(rest)}
	if stream {
		// 这一族的流式也走 SSE，但事件名自带语义（response.output_text.delta 等）；
		// 它**没有** chat 那种 stream_options.include_usage ——用量在
		// response.completed 事件的负载里，无需额外开关。
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

// splitSystemMessages 把序列里的 system 消息抽出来（按原顺序、换行相连），其余原样保留。
//
// 多个 system 段用 `\n\n` 相连而不是覆盖：常驻规则与将来可能加的模块手册是两段独立文本，
// 覆盖会让后一段静默吃掉前一段（规则看着还在，实际只剩一半）。
// 顺序保持原样 —— 稳定前缀的字节稳定性依赖它（docs/16 §3）。
func splitSystemMessages(msgs []aidto.ChatMessage) (string, []aidto.ChatMessage) {
	var b strings.Builder
	rest := make([]aidto.ChatMessage, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == roleSystem {
			// 刻意**不**做 TrimSpace：调用方给的就是要原样发出去的文本，
			// trim 会让「发出去的」与「调用方手里的」差一个尾换行 ——
			// 语义上没差，但前缀的字节稳定性就是由这种细节定义的（docs/16 §3）。
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

// responsesInput 把消息序列翻成 responses 的 input。
//
// 平凡序列（每个元素都是「有角色、无工具字段、正文非空」）回退成纯字符串形态：
// 字符串与「单条 user message」在服务端等价，但字节更短、且与历史实现完全一致。
func responsesInput(msgs []aidto.ChatMessage) any {
	if len(msgs) == 1 && isPlainMessage(msgs[0]) {
		return msgs[0].Content
	}
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		if len(m.ToolCalls) > 0 {
			// assistant 的正文与调用分开成两条：responses 的 function_call 条目没有 content 字段，
			// 硬塞进去会被服务端当未知字段忽略（用户看到的是「模型的话凭空消失」）。
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

// responsesMessageContent 与 chatMessageContent 同职，但分片形状不同：
// 这一族的图片是 {"type":"input_image","image_url":"<url 字符串>"}（**不是对象**），
// 文本是 {"type":"input_text","text":...}。把 url 写成对象会被服务端按未知结构忽略，
// 表现同样是「模型看不到图片」而请求 200。
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

// isPlainMessage 判断一条消息能否用字符串形态表达。
func isPlainMessage(m aidto.ChatMessage) bool {
	// 带图的消息不能退成裸字符串：那样图片会被静默丢掉（上游收到一个字符串，
	// 它不知道里面该有图），而请求本身是 200。
	return m.Role == roleUser && m.ToolCallID == "" && len(m.ToolCalls) == 0 && m.Name == "" && len(m.Images) == 0
}

// responsesTools 把工具声明翻成 responses 的 tools 形状（**扁平**，没有 chat/completions 的
// function 包装层 —— 少一层壳是多数字段名与参数位置都不同的常见坑）。
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

// parseResponsesReply 从 /responses 的响应体里取出正文、工具调用与用量（usage）。
//
// usage 缺失不算错：它只影响调用流水的用量列（Reported=false），正文该回还是要回 ——
// 把「这家没报 usage」判成失败会让一次成功的对话看起来像挂了。
//
// 取值优先级：
//
//  1. 顶层 output_text（OpenAI SDK 的便捷聚合字段，部分网关会带上）；
//  2. 遍历 output[]，拼接所有 type=output_text 的 text 片段，并收集 type=function_call 的调用。
//
// object 不是 response、或状态是 failed 归口 ErrInternal（底层原文只进日志，不上页面）。
// 「正文为空**且**没有工具调用」才归错：模型要调工具时正文本来就是空的，
// 把这种中间态判成失败会让第一次工具调用就报错。
//
// status=incomplete（截断 / content_filter）**不提前返回**：它只说明这轮没跑到 completed，
// 正文可能照样生成，继续走下面的取值路径；真取不到片段时落到末尾唯一的「无正文」出口。
// 提前把 incomplete 归口 ErrInternal 会把「用户给的上限太小」伪装成「服务器内部错误」。
// parseResponsesReasoning 收集 output[] 里所有 reasoning 条目的文本。
//
// 两种承载都读：summary（这一族协议的常见形态）与 content（部分网关直接给 content）。
// 多段用换行相连 —— 模型一次可能给出多段思考，丢掉后面的会让用户只看到开头。
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

// asAnyList 把可能是数组的字段摊成 []any（不是数组时回 nil）。
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
	// usage 缺失不算错（只影响调用流水的用量列），所以先取出来、后面每个出口都带上它。
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

// parseResponsesOutputText 从 output[] 里抽出助手说给用户的话。
//
// 抽出来单独成函数，是因为它有**两个**消费方：非流式的 parseResponsesReply，
// 以及流式的 response.completed 兜底。
//
// 流式那条为什么要兜底：这一族协议在流式下**并非一定**发
// `response.output_text.delta` 事件 —— 实测遇到过一次「只有 completed 事件、
// 里面 output[] 带着完整正文」，而那时只认 delta 的累积器是空的，
// 于是模型明明答了、系统却报「没能拿到回答」。
// 判据：completed 到达时若累积器是空的，就用这一份填上。
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

// parseResponsesToolCalls 从 output[] 里收集 function_call 条目。
//
// 没有名字的条目跳过（执行不了，留着只会白烧一轮查询）；一个都没有时回 nil。
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
