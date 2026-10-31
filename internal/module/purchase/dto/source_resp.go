package purchasedto

import json "go_wp/pkg/jsonx"

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

type SourceSummaryGroupResp struct {
	Type         string `json:"type"`
	RelatedParty bool   `json:"relatedParty"`
	Count        int64  `json:"count"`
}

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
