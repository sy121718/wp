// Package navigationdto 定义 navigation 模块请求/响应结构（0-C）。
// navigation 表示公开站点导航，与后台权限菜单 menu 严格隔离。
package navigationdto

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

// NavigationResp 导航项响应。
type NavigationResp struct {
	ID        string  `json:"id"`
	ProjectID string  `json:"projectId"`
	Title     string  `json:"title"`
	Path      string  `json:"path"`
	Kind      string  `json:"kind"`
	ParentID  *string `json:"parentId"`
	SortOrder int     `json:"sortOrder"`
	// SourceType 菜单项来源：custom/page/article/product/category/block。
	SourceType string `json:"sourceType"`
	// SourceID 来源实体 ID（custom 时为空）。
	SourceID *string `json:"sourceId"`
	// Target 打开方式：self / blank。
	Target string `json:"target"`
	// PanelBlockID 悬浮面板引用的全局块（超级菜单；空 = 无面板）。
	PanelBlockID *string `json:"panelBlockId"`
	// PanelWidth 面板展示宽度 auto / full。
	PanelWidth string `json:"panelWidth"`
	// UpdatedAt 该项 update_time 的精确取值（RFC3339Nano，带时区偏移）——乐观锁 token。
	//
	// 它是 UpdateReq.ExpectedUpdatedAt 应当**原样回带**的值，与 NavigationNode.UpdatedAt 同义：
	// JSON API 与菜单树两条入口给的是同一个值、同一个口径。
	//
	// 此前这里发的是给人看的分钟串（本地「2006-01-02 15:04」），走 JSON API
	//（List/Get/Create/Update）的消费者拿不到可比对的值，只能自己拼秒级串 —— 而 update_time
	// 是微秒精度，秒级串只在恰好落在整秒时命中：乐观锁要么静默失效（该拦的没拦），要么每次
	// 都误报冲突（该过的过不去）。
	//
	// 本字段**只有一种含义**：任何调用方都不该把它当展示文案（要展示请自行按需格式化）。
	UpdatedAt string `json:"updatedAt"`
}

// NavigationNode 导航项树节点（按 parent_id 组装，构建期编译与菜单管理页共用）。
type NavigationNode struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Path       string  `json:"path"`
	SourceType string  `json:"sourceType"`
	SourceID   *string `json:"sourceId"`
	Target     string  `json:"target"`
	SortOrder  int     `json:"sortOrder"`
	// PanelBlockID / PanelWidth 悬浮面板（超级菜单）：面板内容存块，展示形态存菜单项。
	PanelBlockID *string `json:"panelBlockId"`
	PanelWidth   string  `json:"panelWidth"`
	// UpdatedAt 该项 update_time 的精确取值（乐观锁 token，管理页编辑表单原样回带）。
	// 与 NavigationResp.UpdatedAt 同义：两处都是库内真值，模板与 JSON API 两条入口给的值一致。
	UpdatedAt string            `json:"updatedAt"`
	Children  []*NavigationNode `json:"children,omitempty"`
}
