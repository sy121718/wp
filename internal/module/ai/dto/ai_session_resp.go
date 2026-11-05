package aidto

import (
	"go_wp/pkg/utils"
)

type Session struct {
	ID          int64  `json:"id"`
	SessionKey  string `json:"sessionKey"`
	Title       string `json:"title"`
	ProviderKey string `json:"providerKey"`
	ModelID     string `json:"modelId"`
	Status      int16  `json:"status"`

	EventCount    int64          `json:"eventCount"`
	CompactCount  int            `json:"compactCount"`
	ContextTokens int64          `json:"contextTokens"`
	Version       int64          `json:"version"`
	CreateTime    utils.JSONTime `json:"createTime"`
	UpdateTime    utils.JSONTime `json:"updateTime"`
}

type SessionItem struct {
	Seq        int64  `json:"seq"`
	Kind       string `json:"kind"`
	Content    string `json:"content"`
	Tokens     int64  `json:"tokens"`
	Folded     bool   `json:"folded"`
	FoldedFrom int64  `json:"foldedFrom"`
	FoldedTo   int64  `json:"foldedTo"`
}

type SessionDetail struct {
	Session
	Items         []SessionItem `json:"items"`
	VisibleTokens int64         `json:"visibleTokens"`
	VisibleCount  int           `json:"visibleCount"`
}

type SessionEventItem struct {
	Seq            int64          `json:"seq"`
	Kind           string         `json:"kind"`
	SurfaceOp      string         `json:"surfaceOp"`
	ReplaceFromSeq int64          `json:"replaceFromSeq"`
	ReplaceToSeq   int64          `json:"replaceToSeq"`
	Content        string         `json:"content"`
	ContentTokens  int64          `json:"contentTokens"`
	CreateTime     utils.JSONTime `json:"createTime"`

	Meta map[string]any `json:"meta,omitempty"`
}

type AppendEventResult struct {
	Seq     int64   `json:"seq"`
	Session Session `json:"session"`
}

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

type FoldResult struct {
	StartSeq   int64   `json:"startSeq"`
	SummarySeq int64   `json:"summarySeq"`
	EndSeq     int64   `json:"endSeq"`
	Session    Session `json:"session"`
}

type SendMessageResult struct {
	Session        Session          `json:"session"`
	UserEvent      SessionEventItem `json:"userEvent"`
	AssistantEvent SessionEventItem `json:"assistantEvent"`

	ToolEvents []SessionEventItem `json:"toolEvents,omitempty"`
}

type SessionQuery struct {
	Keyword     string
	Status      int
	ProviderKey string
	ModelID     string
	CreateBy    int64
	From        string
	To          string
}

type SessionUsage struct {
	Sessions  int64 `json:"sessions"`
	Events    int64 `json:"events"`
	Tokens    int64 `json:"tokens"`
	Compacts  int64 `json:"compacts"`
	AvgTokens int64 `json:"avgTokens"`

	HitRateReady bool    `json:"hitRateReady"`
	HitRatePct   float64 `json:"hitRatePct"`

	CachedCalls        int64 `json:"cachedCalls"`
	UsageReportedCalls int64 `json:"usageReportedCalls"`

	// CompactCostPct 读作「压缩成本占 token 总量的百分比」，与 Tokens 出自同一份快照。
	// 没有 CompactCostText：百分比文本是显示格式，模板用 percent 函数拼。
	CompactCostPct float64 `json:"compactCostPct"`
}

// TrendSeries 是一个系列（供应商+模型）在窗口内逐日的 token 事实。
//
// 只交事实：逐日 token 数组与总量。没有 Points / DotX / DotY / Color / TotalText ——
// 折线几何与配色是显示格式，归前端按数组自己算；数字缩写也是显示格式。
// service 一旦算好坐标，换图宽/换点数/换配色都要改后端，而且数字会走样。
type TrendSeries struct {
	ProviderKey string `json:"providerKey"`
	ModelID     string `json:"modelId"`

	// OtherCount > 0 表示这是「其余若干系列」的合并项，不是单一模型。
	OtherCount int `json:"otherCount"`

	Total int64 `json:"total"`

	// Values 是按 From..To 逐日的 token 数，长度 = 天数。索引 i 对应 From+i 天（UTC）。
	Values []int64 `json:"values"`
}

type SessionTrend struct {
	From   string        `json:"from"`
	To     string        `json:"to"`
	Peak   int64         `json:"peak"`
	Series []TrendSeries `json:"series"`
}

type SessionFilterOptions struct {
	Providers []string `json:"providers"`
	Models    []string `json:"models"`
	Creators  []int64  `json:"creators"`
}

type SessionTokenUsage struct {
	ContextTokens int64 `json:"contextTokens"`
	Total         int64 `json:"total"`
	Compacts      int64 `json:"compacts"`

	Rows         []SessionTokenRow `json:"rows"`
	HasBreakdown bool              `json:"hasBreakdown"`
}

type SessionTokenRow struct {
	Kind   string `json:"kind"`
	Events int64  `json:"events"`
	Tokens int64  `json:"tokens"`
}

// SessionCallRow 是一次上游调用的记录事实。
//
// 只交事实：调用时刻、耗时毫秒、token 数、状态、错误 key。
// 没有 LatencyText / TokensText / ErrorText —— 缩写、耗时单位、错误文案全是显示格式，
// 由 sessions.html 用模板函数与全局 t 取词；service 里取词会把界面语言写进业务数据。
type SessionCallRow struct {
	At          utils.JSONTime `json:"at"`
	ProviderKey string         `json:"providerKey"`
	ModelID     string         `json:"modelId"`

	LatencyMs int64 `json:"latencyMs"`
	Tokens    int64 `json:"tokens"`
	OK        bool  `json:"ok"`

	ErrorKey string `json:"errorKey"`
}

type SessionCalls struct {
	Total int64            `json:"total"`
	Rows  []SessionCallRow `json:"rows"`
}

type SessionModelUsage struct {
	ProviderKey string `json:"providerKey"`
	ModelID     string `json:"modelId"`
	Events      int64  `json:"events"`
	Tokens      int64  `json:"tokens"`

	Unrecorded bool `json:"unrecorded"`
}

type DialogueTurn struct {
	Role      string `json:"role"`
	Text      string `json:"text"`
	Reasoning string `json:"reasoning,omitempty"`

	ImageLabels []string `json:"imageLabels,omitempty"`
}
