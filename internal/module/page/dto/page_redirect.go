package pagedto

// 重定向管理（审计 SEO-025）：改 URL 留下的旧路径 301 此前只能生效、不能查看与清理。

// RedirectListReq 重定向管理页查询（工程维度；空工程由 service 取第一个工程兜底）。
type RedirectListReq struct {
	ProjectID string `form:"project" json:"projectId"`
}

// RedirectCreateReq 手动新增一条重定向（营销短链 / 站内路径归并）。
type RedirectCreateReq struct {
	ProjectID string `form:"project" json:"projectId" binding:"required"`
	// SourcePath 源路径（旧路径 / 短链），必须未被任何激活、重定向或保留路由占用。
	SourcePath string `form:"sourcePath" json:"sourcePath" binding:"required"`
	// TargetPath 目标路径，必须对应一条已激活路由（页面或自动发布实例的线上路径）。
	TargetPath string `form:"targetPath" json:"targetPath" binding:"required"`
}

// RedirectDeleteReq 删除一条重定向（按源路径定位，不按自增 id —— 页面上认的就是路径）。
type RedirectDeleteReq struct {
	ProjectID string `form:"project" json:"projectId" binding:"required"`
	Path      string `form:"path" json:"path" binding:"required"`
}

// RedirectMergeReq 把一条重定向的跳转链合并为直达（A → B、B → C 合成 A → C）。
type RedirectMergeReq struct {
	ProjectID string `form:"project" json:"projectId" binding:"required"`
	Path      string `form:"path" json:"path" binding:"required"`
}

// RedirectProjectOption 工程下拉项（page 自己的投影，不跨模块传 project dto）。
type RedirectProjectOption struct {
	ID   string
	Name string
}

// RedirectItem 一条重定向的完整视图：DB 侧占用账 + 访问面 redirect.json 事实。
//
// TargetPath 为空表示这条行「已登记占用但访问面上没有重定向产物」—— 未生效，
// 页面上必须与原样生效的条目区分显示（不能假报成功）。
type RedirectItem struct {
	SourcePath string
	TargetPath string
	StatusCode int
	Effective  bool
	// MultiHop 目标本身还是一条重定向的源（链未合并）。
	MultiHop bool
	// Loop 沿链前进会回到起点或重复节点（成环，需要人工处理）。
	Loop bool
	// FinalPath 沿链走到头的落点（MultiHop 时有意义）。
	FinalPath string
	// OwnerKind page / presentation（归属者决定「这条旧路径原本属于谁」）。
	OwnerKind string
	OwnerID   string
	// OwnerTitle 页面标题；展示实例没有标题，前端按 OwnerKind 显示类型文案。
	OwnerTitle string
	UpdatedAt  string
}

// RedirectListResp 管理页一次性要的全部数据（工程列表 + 条目 + 统计）。
type RedirectListResp struct {
	ProjectID      string
	Projects       []RedirectProjectOption
	Items          []RedirectItem
	Total          int
	EffectiveCount int
	MultiHopCount  int
	LoopCount      int
}
