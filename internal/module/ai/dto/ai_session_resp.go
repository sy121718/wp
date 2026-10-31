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
	Sessions      int64  `json:"sessions"`
	Events        int64  `json:"events"`
	Tokens        int64  `json:"tokens"`
	Compacts      int64  `json:"compacts"`
	AvgTokens     int64  `json:"avgTokens"`
	TokensText    string `json:"tokensText"`
	AvgTokensText string `json:"avgTokensText"`

	HitRateReady bool    `json:"hitRateReady"`
	HitRatePct   float64 `json:"hitRatePct"`
	HitRateText  string  `json:"hitRateText"`

	CachedCalls        int64 `json:"cachedCalls"`
	UsageReportedCalls int64 `json:"usageReportedCalls"`

	CompactCostPct  float64 `json:"compactCostPct"`
	CompactCostText string  `json:"compactCostText"`
}

type TrendSeries struct {
	ProviderKey string `json:"providerKey"`
	ModelID     string `json:"modelId"`

	OtherCount int    `json:"otherCount"`
	Total      int64  `json:"total"`
	TotalText  string `json:"totalText"`

	Color  int    `json:"color"`
	Points string `json:"points"`

	Single bool `json:"single"`
	DotX   int  `json:"dotX"`
	DotY   int  `json:"dotY"`
}

type SessionTrend struct {
	From     string        `json:"from"`
	To       string        `json:"to"`
	Peak     int64         `json:"peak"`
	PeakText string        `json:"peakText"`
	Series   []TrendSeries `json:"series"`
}

type SessionFilterOptions struct {
	Providers []string `json:"providers"`
	Models    []string `json:"models"`
	Creators  []int64  `json:"creators"`
}

type SessionTokenUsage struct {
	ContextTokens int64  `json:"contextTokens"`
	Total         int64  `json:"total"`
	TotalText     string `json:"totalText"`
	Compacts      int64  `json:"compacts"`

	Rows         []SessionTokenRow `json:"rows"`
	HasBreakdown bool              `json:"hasBreakdown"`
}

type SessionTokenRow struct {
	Kind   string `json:"kind"`
	Events int64  `json:"events"`
	Tokens int64  `json:"tokens"`
}

type SessionCallRow struct {
	Time        string `json:"time"`
	ProviderKey string `json:"providerKey"`
	ModelID     string `json:"modelId"`

	LatencyText string `json:"latencyText"`
	Tokens      int64  `json:"tokens"`
	TokensText  string `json:"tokensText"`
	OK          bool   `json:"ok"`

	ErrorKey  string `json:"errorKey"`
	ErrorText string `json:"errorText"`
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
	TokensText  string `json:"tokensText"`

	Unrecorded bool `json:"unrecorded"`
}

type DialogueTurn struct {
	Role      string `json:"role"`
	Text      string `json:"text"`
	Reasoning string `json:"reasoning,omitempty"`

	ImageLabels []string `json:"imageLabels,omitempty"`
}
