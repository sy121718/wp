// ai_chat.go — 对话入口的用例：按 provider_key 取一个已启用的供应商，打一次上游拿回文本。
//
// 复用面（ai_client.go 的协议层，本文件不重复实现协议）：
//
//	buildProtocolRequest(ctx, provider, model, input, maxOutputTokens) → 组装一次出站请求，
//	  内部走 applyProviderHeaders(config_data.headers) 与 Authorization: Bearer <明文密钥>；
//	parseProtocolReply(protocol, body) → 按协议解析出文本与用量（未实现的协议回 ErrProtocolUnsupported）。
//
// 密钥口径（与 ai_service.go 头注释一致）：明文只在 buildProtocolRequest 内部解密后进请求头，
// 本文件不接触明文，返回值也不含密钥。
package aiservice

import (
	"context"
	"io"
	"strings"
	"time"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aimodel "go_wp/internal/module/ai/model"
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
	httpReq, berr := s.buildProtocolRequest(reqCtx, provider, model, req.Input, maxTokens)
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
	output, usage, perr := parseProtocolReply(provider.Protocol, body)
	if perr != nil {
		return nil, perr
	}
	// 用量只记上游**上报**的值：这家没报就留 0 + UsageReported=false，
	// 不在这里估算补齐（估算值混进流水会被当成真用量）。
	entry.InputTokens = usage.InputTokens
	entry.OutputTokens = usage.OutputTokens
	entry.TotalTokens = usage.TotalTokens
	entry.UsageReported = usage.Reported

	return &aidto.ChatResult{
		ProviderKey:   provider.ProviderKey,
		Model:         model,
		Output:        output,
		Protocol:      provider.Protocol,
		InputTokens:   usage.InputTokens,
		OutputTokens:  usage.OutputTokens,
		TotalTokens:   usage.TotalTokens,
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
