package navigationdto

// Package navigationdto 定义 navigation 模块请求/响应结构（0-C）。
// navigation 表示公开站点导航，与后台权限菜单 menu 严格隔离。

// CreateReq 新建公开站点导航项。
type CreateReq struct {
	ProjectID string `json:"projectId" binding:"required"`
	Title     string `json:"title" binding:"required"`
	Path      string `json:"path" binding:"required"`
	Kind      string `json:"kind" binding:"required"`
	// ParentID 父导航项 ID（可选，为空表示根导航项）。
	ParentID *string `json:"parentId"`
	// SortOrder 排序权重（越小越靠前）。
	SortOrder int `json:"sortOrder"`
	// SourceType 菜单项来源：custom（手填链接）/page/article/product/category/block。
	SourceType string `json:"sourceType"`
	// SourceID 来源实体 ID（custom 时为空）。
	SourceID *string `json:"sourceId"`
	// Target 打开方式：self / blank。
	Target string `json:"target"`
	// PanelBlockID 悬浮面板引用的全局块（可选；空 = 无面板，超级菜单）。
	PanelBlockID *string `json:"panelBlockId"`
	// PanelWidth 面板展示宽度：auto（跟随内容）/ full（通栏）。
	PanelWidth string `json:"panelWidth"`
}

// UpdateReq 更新公开站点导航项（仅更新传入的非空字段）。
type UpdateReq struct {
	ID        string  `json:"id" binding:"required"`
	Title     *string `json:"title"`
	Path      *string `json:"path"`
	Kind      *string `json:"kind"`
	ParentID  *string `json:"parentId"`
	SortOrder *int    `json:"sortOrder"`
	// SourceType / SourceID / Target 同 CreateReq。
	SourceType *string `json:"sourceType"`
	SourceID   *string `json:"sourceId"`
	Target     *string `json:"target"`
	// PanelBlockID 面板块：nil = 不改动；指向空串 = 清除面板（已有面板要能撤销）。
	PanelBlockID *string `json:"panelBlockId"`
	// PanelWidth 面板宽度 auto / full（nil = 不改动）。
	PanelWidth *string `json:"panelWidth"`
	// ExpectedUpdatedAt 乐观锁：调用方读到的 update_time（精确到微秒的 RFC3339 串）。
	//
	// 后台菜单页与工作台检查器是同一份数据的两个入口，此前是 last-write-wins ——
	// 两处同时改会静默覆盖。带上该值后，库内当前值与之不等即拒绝（打回给人，不自动合并）。
	// nil / 空串 = 不做校验（既有调用方与内部路径的兼容形态）。
	ExpectedUpdatedAt *string `json:"expectedUpdatedAt"`
}

// GetReq 按 ID 查询导航项。
type GetReq struct {
	ID string `form:"id" binding:"required"`
}

// ListReq 按工程（可选 kind）列表。
type ListReq struct {
	ProjectID string `form:"projectId" binding:"required"`
	Kind      string `form:"kind"`
}

// DeleteReq 删除导航项。
type DeleteReq struct {
	ID string `json:"id" binding:"required"`
}

// MoveReq 调整导航项排序（可选：等价于仅改 SortOrder 的 Update，预留快捷入口）。
type MoveReq struct {
	ID        string `json:"id" binding:"required"`
	SortOrder *int   `json:"sortOrder"`
}

// SwapMoveReq 同级上移/下移请求（页面 handler 一次调用即可完成的**原子**交换）。
//
// 为什么不带「期望版本」（不做客户端陈旧锁）：现页面的 move 表单只有 id/dir，没有回带
// update_time。服务端用自己刚读到的树快照当 token，防的是「快照 → 写入」这段窗口内的并发；
// 它**不**宣称防住「页面渲染之后的陈旧操作」—— 那需要把 token 带进表单，属另一批。
type SwapMoveReq struct {
	ID string `json:"id" binding:"required"`
	// ProjectID 调用方认为该项所属的工程。服务端会与真实归属比对，不一致即拒
	// （跨工程一律拒 —— 移动是同一份树内部的相对位置写入，不属于「按 id 直改」）。
	ProjectID string `json:"projectId"`
	// Dir 上移 up / 下移 down。
	Dir string `json:"dir" binding:"required"`
}
