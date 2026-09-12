package maildto

// mail_automation_req.go — 自动化流程请求（issue #38 P3）。

import "encoding/json"

// SaveAutomationReq 新建 / 更新流程。
//
// Definition 是**原始 JSON**而不是结构化结构体：服务端要原样存下来（它是编辑器的数据），
// 只在保存时解析一遍做校验。用 json.RawMessage 可以避免「解析成结构体再序列化回去」
// 让作者没写的字段被悄悄丢掉。
type SaveAutomationReq struct {
	ID          uint64
	Name        string
	Description string
	TriggerType string
	// TriggerParams 触发条件（如 tag_added 匹配的标签）。
	TriggerParams map[string]any
	// Definition 图定义（{"entry":...,"nodes":[...]}）。
	Definition json.RawMessage
	OperatorID uint64
}

// AutomationListReq 流程列表筛选。
type AutomationListReq struct {
	Status   string
	Page     int
	PageSize int
}

// SetAutomationStatusReq 启停流程。
type SetAutomationStatusReq struct {
	ID         uint64
	Status     string
	OperatorID uint64
}

// AutomationPosition 节点在画布上的位置。
type AutomationPosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// SaveAutomationLayoutReq 保存画布位置。
//
// 单独一个接口而不是复用 SaveAutomation：位置不是流程语义，
// **不该推进版本号** —— 否则在画布上挪一下节点，所有正在跑的实例都会变成
// 「版本落后于流程」，排障页面会误导人。
type SaveAutomationLayoutReq struct {
	ID        uint64                        `json:"id"`
	Positions map[string]AutomationPosition `json:"positions"`
}

// AutomationRunReq 手工把联系人加进流程（manual 触发）。
type AutomationRunReq struct {
	AutomationID uint64
	ContactID    uint64
	OperatorID   uint64
}

// AutomationRunListReq 实例列表筛选。
type AutomationRunListReq struct {
	AutomationID uint64
	Status       string
	Page         int
	PageSize     int
}

// AutomationRunDetailResp 实例排障详情。
//
// **Explain 是这个页面存在的理由**：光有 status 字段（running / waiting / failed）没人看得懂
// 「为什么这个人停在这里」。Explain 用一句人话回答：在等什么、等到什么时候、失败在哪一步、为什么。
type AutomationRunDetailResp struct {
	Run            AutomationRunItem `json:"run"`
	AutomationID   uint64            `json:"automationId"`
	AutomationName string            `json:"automationName"`
	Email          string            `json:"email"`
	Name           string            `json:"name"`
	// DoneNodes / TotalNodes 进度：让「卡住了」与「正常在跑」一眼可分。
	DoneNodes  int `json:"doneNodes"`
	TotalNodes int `json:"totalNodes"`
	// Explain 一句话解释当前状态。
	Explain string `json:"explain"`
	// Timeline 节点执行时间线（排障的主视图）。
	Timeline []AutomationNodeLogItem `json:"timeline"`
}
