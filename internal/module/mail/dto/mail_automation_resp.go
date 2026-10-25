package maildto

// mail_automation_resp.go — 自动化流程响应（issue #38 P3）。

import json "go_wp/pkg/jsonx"

// 节点类型在**对外回执**里的取值。
//
// 图定义在 v2 里把 v1 的 `branch` 改名成 `filter`（见 service/automation_definition.go），
// 但回执的消费方不止 v2：画布 JS、表单还原、MCP 工具与外部调用方都按 v1 的名字取值。
// 改名一瞬间就有两套名字，**必须有且只有一处**负责翻译 —— 否则每个消费点各写一遍
// `type == "filter" || type == "branch"`，漏一处就静默退化（表单把分支流程当「还原不了」，
// 画布把两条边当成一条）。
//
// 什么时候能删：画布与表单页都改认 filter 之后。届时本函数与 NodeTypeLegacyBranch
// 一起删除，调用点自然编译失败（比留一个「其实没人用」的兼容层安全）。
const (
	// NodeTypeLegacyBranch v1 里条件分支的类型名（v2 改名为 filter）。
	NodeTypeLegacyBranch = "branch"
	// NodeTypeFilter v2 里条件分支的类型名。
	NodeTypeFilter = "filter"
)

// LegacyNodeType 把 v2 类型名投影回 v1 消费方认识的名字（只翻 filter → branch 这一处）。
func LegacyNodeType(v2Type string) string {
	if v2Type == NodeTypeFilter {
		return NodeTypeLegacyBranch
	}
	return v2Type
}

// AutomationNodeItem 节点（回显用，原样透传 params）。
type AutomationNodeItem struct {
	Key    string         `json:"key"`
	Type   string         `json:"type"`
	Params map[string]any `json:"params,omitempty"`
	Next   string         `json:"next,omitempty"`
	Yes    string         `json:"yes,omitempty"`
	No     string         `json:"no,omitempty"`
	// X / Y 画布位置（P4，引擎完全忽略）。
	X float64 `json:"x,omitempty"`
	Y float64 `json:"y,omitempty"`
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
