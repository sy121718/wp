package pagedto

// RedirectProjectOptionResp 工程下拉项（page 自己的投影，不跨模块传 project dto）。
type RedirectProjectOptionResp struct {
	ID   string
	Name string
}

// RedirectItemResp 一条重定向的完整视图：DB 侧占用账 + 访问面 redirect.json 事实。
//
// TargetPath 为空表示这条行「已登记占用但访问面上没有重定向产物」—— 未生效，
// 页面上必须与原样生效的条目区分显示（不能假报成功）。
type RedirectItemResp struct {
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
	Projects       []RedirectProjectOptionResp
	Items          []RedirectItemResp
	Total          int
	EffectiveCount int
	MultiHopCount  int
	LoopCount      int
}
