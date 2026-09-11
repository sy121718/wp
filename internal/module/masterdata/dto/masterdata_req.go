// masterdata_req.go — 主数据变更记录模块入参（inbound 绑定用，issue #19）。
package masterdatadto

// ListChangeReq 变更记录列表 / 计数的查询条件（全部可选，条件之间是「与」）。
//
// 每条筛选都对一个具体维度：实体类型 / 实体 id / 字段 / 动作 / 操作人 / 关键词 /
// 时间区间。空串表示该维度不过滤（与货源列表同一约定：不把「没填」当成某个具体值）。
type ListChangeReq struct {
	// ProjectID 工程；为空时由 service 解析唯一工程。
	ProjectID string `form:"projectId" json:"projectId"`
	// EntityType 实体类型（product / product_variant / inventory_source）。
	EntityType string `form:"entityType" json:"entityType"`
	// EntityID 实体 id —— 「按实体查询变更历史」的主键维度。
	EntityID string `form:"entityId" json:"entityId"`
	// Field 字段名（如 price / status / sku_code）。
	Field string `form:"field" json:"field"`
	// Action 动作（create / update / delete）。
	Action string `form:"action" json:"action"`
	// Keyword 实体展示名关键词（商品名 / SKU 编码 / 货源名）。
	Keyword string `form:"keyword" json:"keyword"`
	// OperatorID 操作人（登录名）。
	OperatorID string `form:"operatorId" json:"operatorId"`
	// Since / Until 时间区间：since 含端点、until 不含（半开区间，避免「同一天算两次」）。
	Since string `form:"since" json:"since"`
	Until string `form:"until" json:"until"`
	Page  int    `form:"page" json:"page"`
	Size  int    `form:"size" json:"size"`
}

// ListEntityReq 按实体聚合的历史清单查询条件（与 ListChangeReq 同一组维度）。
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

// EntityTimelineReq 单个实体的完整变更时间线（实体类型 + 实体 id 必填）。
type EntityTimelineReq struct {
	ProjectID  string `form:"projectId" json:"projectId"`
	EntityType string `form:"entityType" json:"entityType" binding:"required"`
	EntityID   string `form:"entityId" json:"entityId" binding:"required"`
	Page       int    `form:"page" json:"page"`
	Size       int    `form:"size" json:"size"`
}
