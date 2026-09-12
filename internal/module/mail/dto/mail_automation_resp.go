package maildto

// mail_automation_resp.go — 自动化流程响应（issue #38 P3）。

import "encoding/json"

// AutomationNodeItem 节点（回显用，原样透传 params）。
type AutomationNodeItem struct {
	Key    string         `json:"key"`
	Type   string         `json:"type"`
	Params map[string]any `json:"params,omitempty"`
	Next   string         `json:"next,omitempty"`
	Yes    string         `json:"yes,omitempty"`
	No     string         `json:"no,omitempty"`
}

// AutomationItem 流程条目。
type AutomationItem struct {
	ID            uint64          `json:"id"`
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	TriggerType   string          `json:"triggerType"`
	TriggerParams map[string]any  `json:"triggerParams,omitempty"`
	Definition    json.RawMessage `json:"definition"`
	// Entry 入口节点标识（编辑器回填用）。
	Entry      string               `json:"entry"`
	Nodes      []AutomationNodeItem `json:"nodes"`
	Status     string               `json:"status"`
	Version    int                  `json:"version"`
	CreateTime string               `json:"createTime"`
}

// AutomationListResp 流程列表。
type AutomationListResp struct {
	Items []AutomationItem `json:"items"`
	Total int64            `json:"total"`
}

// AutomationRunItem 运行实例（排障视图的一行）。
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

// AutomationRunListResp 实例列表。
type AutomationRunListResp struct {
	Items  []AutomationRunItem `json:"items"`
	Total  int64               `json:"total"`
	Counts map[string]int64    `json:"counts"`
}

// AutomationNodeLogItem 节点日志（「这个人卡在哪一步」的答案）。
type AutomationNodeLogItem struct {
	NodeKey    string `json:"nodeKey"`
	NodeType   string `json:"nodeType"`
	Status     string `json:"status"`
	Detail     string `json:"detail"`
	CreateTime string `json:"createTime"`
}
