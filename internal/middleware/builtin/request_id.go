package builtin

// request_id.go — 请求标识：生成 / 沿用 / 回写响应头 / 交给日志。
//
// 要解决的问题：一次页面请求会穿过 handler → service → model → pipeline，每一层各写各的
// 日志（34 个 scene）。出问题时要看「这一次请求发生了什么」，过去只能按时间窗口 grep ——
// 并发下必然混进别人的日志。有了 request_id，一条 grep 就能捞出全部相关行。
//
// 三件事只在这里做一次：
//  1. **生成**：上游带 X-Request-Id 就沿用（反向代理 / 网关可能已经生成），否则新建。
//     沿用前必须校验形状 —— 直接信任外部输入等于让人往日志里注入任意值。
//  2. **回写**：写进响应头。没有它，用户报障时说不出「哪一次请求」，日志也就无从捞起。
//  3. **进日志**：把 id 放进 request context，logger 侧（pkg/logger）自动带上。
//
// 第三个是关键的取舍：不要求 34 个日志调用点各自 With("request_id", …)。漏一个就是
// 静默缺口，而「漏了哪个」无法从代码上看出来。让 logger 从 ctx 取，只有「调用方忘了传
// ctx」这一个缺口，而那个缺口 Go 的类型系统会逼着人看见（见 pkg/logger 的 Ctx 系列）。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/logger"
)

// ContextKeyRequestID gin.Context 里存 request id 的键（gin 用字符串键）。
const ContextKeyRequestID = "request_id"

// HeaderRequestID 承载请求标识的头（请求与响应同名）。
const HeaderRequestID = "X-Request-Id"

// requestIDMaxLen 接受的上游 id 长度上限：日志字段不该成为任意长度的注入面。
const requestIDMaxLen = 64

// NewRequestID 生成 128 位随机 id（32 个十六进制字符）。
//
// 不引入第三方依赖：crypto/rand + hex 足够，且失败时也有确定的降级行为。
func NewRequestID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand 失败意味着系统熵源有问题（极罕见）。这时仍要给出一个 id：
		// 宁可重复也不让整条链路失去关联能力，重复会在日志里表现为「两条不相关的
		// 请求共用 id」，比完全没有 id 好排查。
		return "noentropy"
	}
	return hex.EncodeToString(buf[:])
}

// sanitizeRequestID 校验并归一上游带来的 id：只接受 1..64 个 [A-Za-z0-9_.:-]。
//
// 不做「转义后使用」，而是**不合法就不用**：一个被接受的脏 id 会同时进日志字段与响应头，
// 转义规则一旦与消费端（日志检索、header 解析）不一致就会重新撕开口子。丢弃它、
// 自己生成一个，语义上没有任何损失（关联能力只依赖同一次请求内的一致性）。
func sanitizeRequestID(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" || len(s) > requestIDMaxLen {
		return "", false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case c == '_' || c == '.' || c == ':' || c == '-':
		default:
			return "", false
		}
	}
	return s, true
}

// RequestID 返回当前请求的 id（未挂载中间件时为空串）。
//
// 由 handler / 页面出口在需要显式引用时才用；日志不必调它 —— logger 从 ctx 取。
func RequestID(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if v, ok := c.Get(ContextKeyRequestID); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// RequestIDMiddleware 生成/沿用 request id，回写响应头，并放进 request context。
//
// 顺序要求：**必须排在 Recovery 之后、所有会写日志的中间件之前**。
// 排在最前会让 panic 恢复时拿不到 id（Recovery 自己也要记日志）；
// 排在限流/日志之后，那些中间件的日志就失去关联。
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := sanitizeRequestID(c.GetHeader(HeaderRequestID))
		if !ok {
			id = NewRequestID()
		}
		// 写回请求头：下游若用 c.Request.Header 取（而不是 gin.Context）也能拿到同一个值。
		c.Request.Header.Set(HeaderRequestID, id)
		// 响应头用 Set 而不是 Add：上游若已带了同名头（极少数网关会），不能出现两个值。
		c.Header(HeaderRequestID, id)
		c.Set(ContextKeyRequestID, id)
		// 写进 request context：logger 的 Ctx 系列从这里取，34 个 scene 都不用改调用点。
		c.Request = c.Request.WithContext(logger.WithRequestID(c.Request.Context(), id))
		c.Next()
	}
}

// RequestIDFromContext 从任意 context 取 request id（未设置返回空串）。
//
// 供非 HTTP 场景（队列任务、CLI）复用同一套关联能力：
// 任务体内用 logger.Ctx(ctx) 就能带上发起请求的 id。
func RequestIDFromContext(ctx context.Context) string {
	return logger.RequestIDFrom(ctx)
}
