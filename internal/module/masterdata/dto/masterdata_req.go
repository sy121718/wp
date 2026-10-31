package masterdatadto

type ListChangeReq struct {
	ProjectID string `form:"projectId" json:"projectId"`

	EntityType string `form:"entityType" json:"entityType"`

	EntityID string `form:"entityId" json:"entityId"`

	Field string `form:"field" json:"field"`

	Action string `form:"action" json:"action"`

	Keyword string `form:"keyword" json:"keyword"`

	OperatorID string `form:"operatorId" json:"operatorId"`

	Since string `form:"since" json:"since"`
	Until string `form:"until" json:"until"`
	Page  int    `form:"page" json:"page"`
	Size  int    `form:"size" json:"size"`
}

type ListEntityReq struct {
	ProjectID  string `form:"projectId" json:"projectId"`
	EntityType string `form:"entityType" json:"entityType"`
	EntityID   string `form:"entityId" json:"entityId"`
	Field      string `form:"field" json:"field"`
	Action     string `form:"action" json:"action"`
	Keyword    string `form:"keyword" json:"keyword"`
	OperatorID string `form:"operatorId" json:"operatorId"`
	Since      string `form:"since" json:"since"`
	Until      string `form:"until" json:"until"`
	Page       int    `form:"page" json:"page"`
	Size       int    `form:"size" json:"size"`
}

type EntityTimelineReq struct {
	ProjectID  string `form:"projectId" json:"projectId"`
	EntityType string `form:"entityType" json:"entityType" binding:"required"`
	EntityID   string `form:"entityId" json:"entityId" binding:"required"`
	Page       int    `form:"page" json:"page"`
	Size       int    `form:"size" json:"size"`
}
