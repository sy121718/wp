package navigationdto

// Package navigationdto 定义 navigation 模块请求/响应结构（0-C）。
// navigation 表示公开站点导航，与后台权限菜单 menu 严格隔离。

// SwapMoveResp 同级交换结果。
type SwapMoveResp struct {
	// Moved false = 已在首/末位，本次零写入（页面按原有的「无需调整」提示走，行为与修复前一致）。
	Moved bool `json:"moved"`
	// Reason 未位移的原因：""（已交换）/ boundary（首/末位）。
	// 并列（sort_order 相等）不是这里的一种取值：它走业务错误 ErrSortTied（零写），
	// 让页面用失败样式说清「没发生位移」，而不是回一个「已完成」。
	Reason string `json:"reason,omitempty"`
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
