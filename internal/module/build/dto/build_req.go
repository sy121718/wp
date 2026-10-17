package builddto

// Package builddto build 模块请求/响应结构（构建任务队列，审计 DB-007 / DB-01）。

// EnqueueReq 入队请求。
//
// Intent 留空时按 dependency（依赖重建）入队：当前唯一的入队来源是依赖失效扇出，
// 人工构建要显式传 manual —— 默认值必须站在**多数且更安全**的一侧，
// 依赖重建走的是「有就排上、没有就算了」的幂等语义，误判成人工构建会让后台列表失真。
type EnqueueReq struct {
	SourceType string `json:"sourceType" binding:"required"`
	SourceID   string `json:"sourceId" binding:"required"`
	// ProjectID 任务所属工程（显式入库，消费侧不必再反推来源行）。
	ProjectID string `json:"projectId"`
	// Lang 构建语言；留空 = 默认语言。
	Lang           string `json:"lang"`
	Intent         string `json:"intent"`
	DraftVersion   int64  `json:"draftVersion"`
	BuildInputHash string `json:"buildInputHash"`
}

// ListReq 任务列表请求。
type ListReq struct {
	// ProjectID 只列该工程的任务（空 = 全队列）。build_jobs **没有** RLS 策略，
	// 工程过滤只能靠这个条件，见 build/model 包注释。
	ProjectID string `form:"project" json:"projectId"`
	Status    string `form:"status" json:"status"`
	Limit     int    `form:"limit" json:"limit"`
}
