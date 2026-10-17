package pagedto

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
