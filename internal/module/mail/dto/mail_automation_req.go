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

// AutomationRunReq 手工把联系人加进流程（manual 触发）。
type AutomationRunReq struct {
	AutomationID uint64
	ContactID    uint64
	OperatorID   uint64
}
