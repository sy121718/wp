package maildto

import json "go_wp/pkg/jsonx"

const (
	NodeTypeLegacyBranch = "branch"

	NodeTypeFilter = "filter"
)

func LegacyNodeType(v2Type string) string {
	if v2Type == NodeTypeFilter {
		return NodeTypeLegacyBranch
	}
	return v2Type
}

type AutomationNodeItem struct {
	Key    string         `json:"key"`
	Type   string         `json:"type"`
	Params map[string]any `json:"params,omitempty"`
	Next   string         `json:"next,omitempty"`
	Yes    string         `json:"yes,omitempty"`
	No     string         `json:"no,omitempty"`

	X float64 `json:"x,omitempty"`
	Y float64 `json:"y,omitempty"`
}

type AutomationItem struct {
	ID            uint64          `json:"id"`
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	TriggerType   string          `json:"triggerType"`
	TriggerParams map[string]any  `json:"triggerParams,omitempty"`
	Definition    json.RawMessage `json:"definition"`

	Entry string               `json:"entry"`
	Nodes []AutomationNodeItem `json:"nodes"`

	TriggerFrequency string `json:"triggerFrequency"`
	ExitOnReply      bool   `json:"exitOnReply"`
	Status           string `json:"status"`
	Version          int    `json:"version"`
	CreateTime       string `json:"createTime"`
}

type AutomationListResp struct {
	Items []AutomationItem `json:"items"`
	Total int64            `json:"total"`
}

type AutomationRunItem struct {
	ID           uint64 `json:"id"`
	AutomationID uint64 `json:"automationId"`
	ContactID    uint64 `json:"contactId"`
	Email        string `json:"email"`
	Status       string `json:"status"`
	CurrentNode  string `json:"currentNode"`
	NextRunAt    string `json:"nextRunAt"`
	ErrorMessage string `json:"errorMessage"`
	TriggerEvent string `json:"triggerEvent"`
	StartedAt    string `json:"startedAt"`
	FinishedAt   string `json:"finishedAt"`
}

type AutomationRunListResp struct {
	Items  []AutomationRunItem `json:"items"`
	Total  int64               `json:"total"`
	Counts map[string]int64    `json:"counts"`
}

type AutomationNodeLogItem struct {
	NodeKey    string `json:"nodeKey"`
	NodeType   string `json:"nodeType"`
	Status     string `json:"status"`
	Detail     string `json:"detail"`
	CreateTime string `json:"createTime"`
}
