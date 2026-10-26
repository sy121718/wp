// source_req.go — 货源的增删改查入参与关联方统计请求。
package purchasedto

import (
	json "go_wp/pkg/jsonx"
)

// CreateSourceReq 新建货源（外部供应商 / 集团内关联公司 / 自家工厂）。
//
// Type 只认 external / internal；RelatedParty 为 nil 时按类型取默认 ——
// 内部货源恒为关联方（内部交易必须能被关联方报表捕获），外部默认非关联方。
// SettlePrice 是内部结算价，只允许出现在内部货源上。
// Config 是**异构对接扩展信息**（JSON 对象，不同来源字段形状各不相同）。
type CreateSourceReq struct {
	ProjectID    string          `json:"projectId" form:"projectId"`
	Code         string          `json:"code" form:"code" binding:"required"`
	Name         string          `json:"name" form:"name" binding:"required"`
	Type         string          `json:"type" form:"type"`
	RelatedParty *bool           `json:"relatedParty" form:"relatedParty"`
	SettlePrice  *float64        `json:"settlePrice" form:"settlePrice"`
	Status       string          `json:"status" form:"status"`
	Config       json.RawMessage `json:"config" form:"config"`
	Sort         int             `json:"sort" form:"sort"`
	Metadata     json.RawMessage `json:"metadata" form:"metadata"`
	// OperatorID 操作人：由 inbound 从会话覆盖写入，客户端传入的值被忽略
	// （json 标签为 "-"，不可由外部指定）。变更记录记「谁改的」，
	// 统一取会话里的登录名；缺失时为空串（留痕字段允许为空）。
	OperatorID string `json:"-"`
}

// UpdateSourceReq 修改货源（逐字段可选；nil = 本次不改）。
//
// ClearSettlePrice 显式清空结算价 —— 指针为 nil 表示「不改」，没有它就无法把值改回 NULL。
type UpdateSourceReq struct {
	ID string `json:"id" form:"id" binding:"required"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID        string          `json:"projectId" form:"projectId"`
	Code             *string         `json:"code" form:"code"`
	Name             *string         `json:"name" form:"name"`
	Type             *string         `json:"type" form:"type"`
	RelatedParty     *bool           `json:"relatedParty" form:"relatedParty"`
	SettlePrice      *float64        `json:"settlePrice" form:"settlePrice"`
	ClearSettlePrice bool            `json:"clearSettlePrice" form:"clearSettlePrice"`
	Status           *string         `json:"status" form:"status"`
	Config           json.RawMessage `json:"config" form:"config"`
	Sort             *int            `json:"sort" form:"sort"`
	Metadata         json.RawMessage `json:"metadata" form:"metadata"`
	// OperatorID 操作人：由 inbound 从会话覆盖写入（同 CreateSourceReq）。
	OperatorID string `json:"-"`
}

// GetSourceReq 按 ID 查询货源。
type GetSourceReq struct {
	ID string `form:"id" binding:"required"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID string `form:"projectId"`
}

// DeleteSourceReq 删除货源。
type DeleteSourceReq struct {
	ID string `json:"id" form:"id" binding:"required"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID string `json:"projectId" form:"projectId"`
	// OperatorID 操作人：由 inbound 从会话覆盖写入（同 CreateSourceReq）。
	OperatorID string `json:"-"`
}

// ListSourceReq 货源列表（报表区分维度直接落在查询上）。
//
// RelatedParty 是三态字符串："" 全部 / "true" 仅关联方 / "false" 仅非关联方 ——
// 用字符串而不是 *bool，是因为 GET 查询里「参数缺失」与「参数为空」必须能区分开。
type ListSourceReq struct {
	ProjectID       string `form:"projectId"`
	Type            string `form:"type"`
	RelatedParty    string `form:"relatedParty"`
	Status          string `form:"status"`
	Keyword         string `form:"keyword"`
	IncludeDisabled bool   `form:"includeDisabled"`
	Page            int    `form:"page"`
	Size            int    `form:"size"`
}

// SourceSummaryReq 货源关联方统计（按类型 × 关联方分组计数）。
type SourceSummaryReq struct {
	ProjectID string `form:"projectId"`
}

// SourceListQueryReq 货源页的查询串（筛选 + 分页 + 工程）。
//
// Status 有第四个取值 all（连停用的一起列），由页面常量 sourceStatusFilterAll 表达。
type SourceListQueryReq struct {
	ProjectID    string `form:"project"`
	Type         string `form:"type"`
	RelatedParty string `form:"relatedParty"`
	Status       string `form:"status"`
	Keyword      string `form:"keyword"`
	Page         int    `form:"page"`
	Limit        int    `form:"limit"`
}
