package maildto

import json "go_wp/pkg/jsonx"

type SaveAutomationReq struct {
	ID          uint64
	Name        string
	Description string
	TriggerType string

	TriggerParams map[string]any

	TriggerFrequency string

	ExitOnReply *bool

	Definition json.RawMessage
	OperatorID uint64
}

type AutomationListReq struct {
	Status   string
	Page     int
	PageSize int
}

type SetAutomationStatusReq struct {
	ID         uint64
	Status     string
	OperatorID uint64
}

type AutomationPosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type SaveAutomationLayoutReq struct {
	ID        uint64                        `json:"id" form:"id"`
	Positions map[string]AutomationPosition `json:"positions" form:"positions"`
}

type AutomationRunReq struct {
	AutomationID uint64
	ContactID    uint64
	OperatorID   uint64
}

type AutomationRunListReq struct {
	AutomationID uint64
	Status       string
	Page         int
	PageSize     int
}

type AutomationRunDetailResp struct {
	Run            AutomationRunItem `json:"run"`
	AutomationID   uint64            `json:"automationId"`
	AutomationName string            `json:"automationName"`
	Email          string            `json:"email"`
	Name           string            `json:"name"`

	DoneNodes  int `json:"doneNodes"`
	TotalNodes int `json:"totalNodes"`

	Explain string `json:"explain"`

	Timeline []AutomationNodeLogItem `json:"timeline"`
}
