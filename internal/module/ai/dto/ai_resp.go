package aidto

import (
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/utils"
)

type ModelEntry struct {
	ID              string   `json:"id"`
	DisplayName     string   `json:"displayName"`
	ContextWindow   int64    `json:"contextWindow"`
	MaxOutputTokens int64    `json:"maxOutputTokens"`
	InputTypes      []string `json:"inputTypes"`
}

type Provider struct {
	ID          int64          `json:"id"`
	ProviderKey string         `json:"providerKey"`
	DisplayName string         `json:"displayName"`
	BaseURL     string         `json:"baseUrl"`
	Protocol    string         `json:"protocol"`
	Status      int            `json:"status"`
	Sort        int            `json:"sort"`
	Models      []ModelEntry   `json:"models"`
	HasAPIKey   bool           `json:"hasApiKey"`
	Version     int64          `json:"version"`
	UpdateTime  utils.JSONTime `json:"updateTime"`
}

type FetchModelsResult struct {
	Provider *Provider `json:"provider"`
	Added    []string  `json:"added"`
	Fetched  int       `json:"fetched"`
}

// AppendedModelsResp 是「批量追加」成功后的 JSON 事实：
// 实际新增了几行 + 保存后的提供者（含新 version，前端下次保存要用）。
type AppendedModelsResp struct {
	Added    int      `json:"added"`
	Provider Provider `json:"provider"`
}

type ModelCandidates struct {
	Provider   *Provider `json:"provider"`
	Candidates []string  `json:"candidates"`
	Existing   []string  `json:"existing"`
	Fetched    int       `json:"fetched"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`

	Images []string `json:"images,omitempty"`

	ToolCalls []ToolCall `json:"toolCalls,omitempty"`

	ToolCallID string `json:"toolCallId,omitempty"`

	Name string `json:"name,omitempty"`
}

type ToolCall struct {
	ID string `json:"id"`

	Name string `json:"name"`

	Arguments string `json:"arguments"`
}

type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type ChatResult struct {
	ProviderKey string `json:"provider_key"`
	Model       string `json:"model"`
	Output      string `json:"output"`

	Reasoning string `json:"reasoning,omitempty"`
	Protocol  string `json:"protocol"`

	ToolCalls []ToolCall `json:"tool_calls,omitempty"`

	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`

	CachedTokens  int64 `json:"cached_tokens"`
	UsageReported bool  `json:"usage_reported"`
}
