package productdto

import "go_wp/pkg/utils"

// PublishAttemptListResp 商品的发布尝试查询结果；页面自行格式化时刻、耗时与语言。
type PublishAttemptListResp struct {
	Ready   bool                    `json:"ready"`
	Rows    []PublishAttemptRowResp `json:"rows"`
	Total   int64                   `json:"total"`
	Page    int                     `json:"page"`
	Size    int                     `json:"size"`
	HasMore bool                    `json:"hasMore"`
}

// PublishAttemptRowResp 一次发布尝试的业务事实。
type PublishAttemptRowResp struct {
	ID              int64          `json:"id"`
	CreatedAt       utils.JSONTime `json:"createdAt"`
	Trigger         string         `json:"trigger"`
	Outcome         string         `json:"outcome"`
	Langs           []string       `json:"langs"`
	Attempts        int            `json:"attempts"`
	ArtifactVersion int64          `json:"artifactVersion"`
	ArtifactHash    string         `json:"artifactHash"`
	OperatorName    string         `json:"operatorName"`
	DurationMs      int64          `json:"durationMs"`
}

// PublishAttemptListReq 商品发布历史的查询条件。
type PublishAttemptListReq struct {
	ProjectID string `form:"project"`
	ProductID string `form:"productId"`
	Page      int    `form:"page"`
	Size      int    `form:"size"`
}
