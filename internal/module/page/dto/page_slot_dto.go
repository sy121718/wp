package pagedto

// page_slot_dto.go — 系统页面槽位的请求与响应。

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

// SiteSlotItem 一个槽位的当前状态（未绑定的槽位也会出现在列表里）。
//
// Path 与 Published 分开给：**绑定存在**与**访问面真的有产物**是两件事 ——
// 页面可以是草稿、可以已下线，那时路径是空的，链接生成方据此降级而不是输出死链。
type SiteSlotItem struct {
	Slot     string `json:"slot"`
	SlotName string `json:"slotName"`
	Usage    string `json:"usage"`
	Bound    bool   `json:"bound"`
	PageID   string `json:"pageId"`
	// DraftPath 页面草稿路径：**始终有值**，页面的辨认就靠它
	//（页面没有「标题」这个概念，路径是它唯一稳定的身份）。
	DraftPath string `json:"draftPath"`
	// Path 当前语言下**已发布**的访问路径（未发布为空）。
	Path string `json:"path"`
	// Published 是否已发布（绑定在但没发布 = 链接会 404）。
	Published bool `json:"published"`
	// PageDeleted 页面被删了但绑定还在（引用了不存在的页面）。
	PageDeleted bool `json:"pageDeleted"`
}

// SiteSlotListResp 槽位列表。
type SiteSlotListResp struct {
	Items      []SiteSlotItem `json:"items"`
	BoundCount int            `json:"boundCount"`
	Total      int            `json:"total"`
	// Lang 解析路径用的语言（后台展示与构建一致）。
	Lang string `json:"lang"`
}
