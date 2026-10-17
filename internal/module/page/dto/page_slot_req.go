package pagedto

// SiteSlotListReq 列槽位绑定。
type SiteSlotListReq struct {
	ProjectID string `json:"projectId" form:"projectId" binding:"required"`
	// Lang 解析线上路径用的语言；留空用站点默认语言。
	Lang string `json:"lang" form:"lang"`
}

// SiteSlotBindReq 绑定槽位到页面。
type SiteSlotBindReq struct {
	ProjectID string `json:"projectId" form:"projectId" binding:"required"`
	Slot      string `json:"slot" form:"slot" binding:"required"`
	PageID    string `json:"pageId" form:"pageId" binding:"required"`
}

// SiteSlotUnbindReq 解绑槽位。
type SiteSlotUnbindReq struct {
	ProjectID string `json:"projectId" form:"projectId" binding:"required"`
	Slot      string `json:"slot" form:"slot" binding:"required"`
}
