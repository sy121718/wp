// source_resp.go — 货源出参与关联方统计。
package purchasedto

import json "go_wp/pkg/jsonx"

// SourceResp 货源（含类型与关联方标志，两者共同构成报表区分维度）。
//
// SettlePrice 仅内部货源有值（自产商品的成本口径）；Config 是异构对接扩展信息。
type SourceResp struct {
	ID           string          `json:"id"`
	ProjectID    string          `json:"projectId"`
	Code         string          `json:"code"`
	Name         string          `json:"name"`
	Type         string          `json:"type"`
	RelatedParty bool            `json:"relatedParty"`
	SettlePrice  *float64        `json:"settlePrice"`
	Status       string          `json:"status"`
	Config       json.RawMessage `json:"config"`
	Sort         int             `json:"sort"`
	CreatedAt    string          `json:"createdAt"`
	UpdatedAt    string          `json:"updatedAt"`
}

// SourceSummaryGroupResp 一个「类型 × 关联方」分组的计数。
type SourceSummaryGroupResp struct {
	Type         string `json:"type"`
	RelatedParty bool   `json:"relatedParty"`
	Count        int64  `json:"count"`
}

// SourceSummaryResp 货源关联方统计（验收 4「关联方标志可用于报表区分」的数据出口）。
//
// Internal / External 是类型口径，RelatedParty / Unrelated 是关联交易口径 ——
// 两组数字交叉即「内部且关联」「外部但关联」等组合，Groups 给出完整交叉表。
type SourceSummaryResp struct {
	ProjectID    string                    `json:"projectId"`
	Total        int64                     `json:"total"`
	Internal     int64                     `json:"internal"`
	External     int64                     `json:"external"`
	RelatedParty int64                     `json:"relatedParty"`
	Unrelated    int64                     `json:"unrelated"`
	SettlePriced int64                     `json:"settlePriced"`
	Groups       []*SourceSummaryGroupResp `json:"groups"`
}
