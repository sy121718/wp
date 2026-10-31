package productdto

import "go_wp/pkg/utils"

type PublishAttemptListResp struct {
	Ready   bool                    `json:"ready"`
	Rows    []PublishAttemptRowResp `json:"rows"`
	Total   int64                   `json:"total"`
	Page    int                     `json:"page"`
	Size    int                     `json:"size"`
	HasMore bool                    `json:"hasMore"`
}

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

type PublishAttemptListReq struct {
	ProjectID string `form:"project"`
	ProductID string `form:"productId"`
	Page      int    `form:"page"`
	Size      int    `form:"size"`
}
