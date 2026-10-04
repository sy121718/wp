package aidto

import "go_wp/pkg/utils"

// ai_session_dto.go — AI 会话层的请求 / 响应形状（会话头、投影项、追加与折叠）。
//
// 时间字段一律 utils.JSONTime（对外 JSON 只到秒，AGENTS.md「时间列」口径）。
// 投影项不借用 model 实体：model 只回实体，service 负责翻译成这里的形状。

// Session 会话头（列表项与详情共用）。
type Session struct {
	ID          int64  `json:"id"`
	SessionKey  string `json:"sessionKey"`
	Title       string `json:"title"`
	ProviderKey string `json:"providerKey"`
	ModelID     string `json:"modelId"`
	Status      int16  `json:"status"`
	// EventCount = 已分配的最大事件序号（= 事件条数，序号无洞）。
	EventCount    int64          `json:"eventCount"`
	CompactCount  int            `json:"compactCount"`
	ContextTokens int64          `json:"contextTokens"`
	Version       int64          `json:"version"`
	CreateTime    utils.JSONTime `json:"createTime"`
	UpdateTime    utils.JSONTime `json:"updateTime"`
}

// SessionItem 投影里的一项：要么是一条事件，要么是一次折叠留下的摘要块。
//
// Folded=true 时 Content 是模型写的摘要，FoldedFrom/FoldedTo 是它替换掉的序号区间
// （前端据此渲染成可展开的折叠块；展开读原文走事件日志接口，不在这里展开）。
type SessionItem struct {
	Seq        int64  `json:"seq"`
	Kind       string `json:"kind"`
	Content    string `json:"content"`
	Tokens     int64  `json:"tokens"`
	Folded     bool   `json:"folded"`
	FoldedFrom int64  `json:"foldedFrom"`
	FoldedTo   int64  `json:"foldedTo"`
}

// SessionDetail 会话详情 = 会话头 + 当前投影 + 计量。
//
// VisibleTokens 是投影重算出来的可见 token 数（与 ContextTokens 的区别：前者实时算，
// 后者是最后一次写入时落库的值；两者不一致说明有人在两次写入之间直接改了表）。
type SessionDetail struct {
	Session
	Items         []SessionItem `json:"items"`
	VisibleTokens int64         `json:"visibleTokens"`
	VisibleCount  int           `json:"visibleCount"`
}

// SessionEventItem 事件日志里的一条原始记录（运维视图：压缩后原文仍在这里）。
type SessionEventItem struct {
	Seq            int64          `json:"seq"`
	Kind           string         `json:"kind"`
	SurfaceOp      string         `json:"surfaceOp"`
	ReplaceFromSeq int64          `json:"replaceFromSeq"`
	ReplaceToSeq   int64          `json:"replaceToSeq"`
	Content        string         `json:"content"`
	ContentTokens  int64          `json:"contentTokens"`
	CreateTime     utils.JSONTime `json:"createTime"`
}

// AppendEventReq 追加一条事件。
//
// SessionKey 与 SessionID 二选一：给了 SessionKey 走「按客户端的会话标识续写或新建」，
// 给了 SessionID 直接写指定会话。两者都空是参数错误。
type AppendEventReq struct {
	SessionKey string
	SessionID  int64
	Kind       string
	Content    string
	// Tokens <= 0 时按字符数粗估（宁可高估：它是触发折叠的判断依据，低估会让会话涨过头）。
	Tokens int64
	Meta   map[string]any
	UserID int64
	// ProviderKey / ModelID 仅在「因 SessionKey 新建会话」时写入会话头；
	// 命中已有会话时它们参与绑定一致性校验（非空且与库里不一致 → 拒绝，见 ErrSessionProviderMismatch）。
	ProviderKey string
	ModelID     string
	// Title 仅在「因 SessionKey 新建会话」时用作初始标题（通常传首条用户消息的前若干字）。
	Title string
}

// AppendEventResult 追加结果：分配到的序号 + 追加后的会话头。
type AppendEventResult struct {
	Seq     int64   `json:"seq"`
	Session Session `json:"session"`
}

// FoldPlanReq 计算折叠建议区间的入参。
type FoldPlanReq struct {
	SessionID int64
	// KeepRecent 保留最近多少条不参与折叠（尾部工作集；<=0 时用默认值）。
	KeepRecent int
}

// FoldPlanResult 折叠建议：从哪折到哪、折叠前多少 token、折完大概剩多少、值不值得。
//
// Excerpt 是待折叠段的纯文本（供写摘要用）；Worthwhile=false 表示净收益为负或区间不足，
// 调用方应当放弃这次折叠（口径：短会话不值得压，见 docs/16 §3.1）。
type FoldPlanResult struct {
	SessionID      int64  `json:"sessionId"`
	FromSeq        int64  `json:"fromSeq"`
	ToSeq          int64  `json:"toSeq"`
	SegmentTokens  int64  `json:"segmentTokens"`
	EstimatedAfter int64  `json:"estimatedAfter"`
	EstimatedSave  int64  `json:"estimatedSave"`
	Excerpt        string `json:"excerpt"`
	Worthwhile     bool   `json:"worthwhile"`
	Reason         string `json:"reason"`
}

// FoldReq 提交一次折叠：区间 + 模型写好的摘要。
type FoldReq struct {
	SessionID int64
	FromSeq   int64
	ToSeq     int64
	Summary   string
	// SummaryTokens <= 0 时按字符数粗估。
	SummaryTokens int64
	UserID        int64
}

// FoldResult 折叠结果：三条事件（开始 / 摘要 / 结束）的序号 + 折叠后的会话头。
type FoldResult struct {
	StartSeq   int64   `json:"startSeq"`
	SummarySeq int64   `json:"summarySeq"`
	EndSeq     int64   `json:"endSeq"`
	Session    Session `json:"session"`
}

// RenameSessionReq 改会话标题 / 切换当前模型 / 归档（三件事共用乐观锁版本号）。
type RenameSessionReq struct {
	ID          int64
	Title       string
	ProviderKey string
	ModelID     string
	Status      int16
	Version     int64
	UserID      int64
}

// SendMessageReq 会话页「发消息」：写 user 事件 → 取会话上下文投影 → 打一次模型 → 写 assistant 事件。
//
// SessionID 与 SessionKey 二选一：给 SessionID 就往这条已有会话里发；只给 SessionKey 时
// 走 EnsureSession 续写或新建（ProviderKey / ModelID 同时是新建会话的会话头）。
//
// 这里不带 binding:"required"：空值与「没选模型」都要回可翻译的业务 key（见 service 的校验），
// 交给框架的 required 会变成不可控的绑定错误文本。
type SendMessageReq struct {
	SessionID       int64  `json:"sessionId" form:"sessionId"`
	SessionKey      string `json:"sessionKey" form:"sessionKey"`
	ProviderKey     string `json:"providerKey" form:"providerKey" binding:"max=50"`
	Model           string `json:"model" form:"model" binding:"max=200"`
	Input           string `json:"input" form:"input" binding:"max=200000"`
	MaxOutputTokens int64  `json:"maxOutputTokens" form:"maxOutputTokens"`
	UserID          int64  `json:"-"`
}

// SendMessageResult 一次「发消息」的结果：会话头 + 落下来的两条事件（用户输入 / 模型回复）。
//
// 两条事件都带分配到的序号与内容 token，前端据此原地渲染，不必再回查事件日志。
type SendMessageResult struct {
	Session        Session          `json:"session"`
	UserEvent      SessionEventItem `json:"userEvent"`
	AssistantEvent SessionEventItem `json:"assistantEvent"`
}
